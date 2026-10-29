//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type automationResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type automationClient struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *bufio.Reader
}

func TestMyGoAutomationDrivesNativeUIAndCapturesIt(t *testing.T) {
	if os.Getenv("BIU_RUN_MYGO_E2E") != "1" {
		t.Skip("设置 BIU_RUN_MYGO_E2E=1 才运行真窗口 E2E")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socketDir, socket, err := makeAutomationSocket()
	if err != nil {
		t.Fatalf("创建自动化 socket：%v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	var buildOut, buildErr bytes.Buffer
	fixture := filepath.Join(socketDir, "fixture")
	build := exec.Command("go", "build", "-tags", "e2e", "-o", fixture, ".")
	build.Dir = fixtureDir()
	build.Stdout = &buildOut
	build.Stderr = &buildErr
	if err := build.Run(); err != nil {
		t.Fatalf("构建 MyGo fixture：%v\\nstdout:\\n%s\\nstderr:\\n%s", err, buildOut.String(), buildErr.String())
	}
	cmd := exec.CommandContext(ctx, fixture)
	cmd.Env = append(os.Environ(), "MYGO_AUTOMATION="+socket)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 MyGo fixture：%v", err)
	}
	defer func() {
		cancel()
		waitForFixtureExit(t, cmd)
	}()

	conn := waitForAutomationSocket(t, socket, cmd, &stdout, &stderr)
	defer conn.Close()
	client := &automationClient{conn: conn, enc: json.NewEncoder(conn), dec: bufio.NewReader(conn)}

	var windows struct {
		Windows []struct {
			ID   int    `json:"id"`
			Kind string `json:"kind"`
		} `json:"windows"`
	}
	callAutomation(t, client, 1, "windows", nil, &windows)
	if len(windows.Windows) != 1 || windows.Windows[0].Kind != "content" {
		t.Fatalf("windows = %+v，want 一个 native content 窗口", windows.Windows)
	}
	window := windows.Windows[0].ID

	var tree struct {
		Nodes []struct {
			Key   any    `json:"key"`
			Label string `json:"label"`
		} `json:"nodes"`
	}
	callAutomation(t, client, 2, "tree", map[string]any{"window": window}, &tree)
	assertAutomationNode(t, tree.Nodes, "count", "Count: 0")
	assertAutomationNode(t, tree.Nodes, "increment", "Increment")

	callAutomation(t, client, 3, "act", map[string]any{
		"window": window,
		"key":    "increment",
		"action": "press",
	}, nil)
	callAutomation(t, client, 4, "idle", map[string]any{"window": window, "timeout": 5000}, nil)
	callAutomation(t, client, 5, "tree", map[string]any{"window": window}, &tree)
	assertAutomationNode(t, tree.Nodes, "count", "Count: 1")

	var capture struct {
		PNG    string `json:"png"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	callAutomation(t, client, 6, "capture", map[string]any{"window": window}, &capture)
	if capture.Width <= 0 || capture.Height <= 0 {
		t.Fatalf("capture size = %dx%d", capture.Width, capture.Height)
	}
	if _, err := base64.StdEncoding.DecodeString(capture.PNG); err != nil {
		t.Fatalf("capture png 不是 base64：%v", err)
	}

	callAutomation(t, client, 7, "act", map[string]any{
		"window": window,
		"key":    "quit",
		"action": "press",
	}, nil)
}

func makeAutomationSocket() (string, string, error) {
	base := automationSocketBase()
	dir, err := os.MkdirTemp(base, "mg-")
	if err != nil {
		return "", "", err
	}
	return dir, filepath.Join(dir, "a.sock"), nil
}

func fixtureDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("定位 automation E2E 文件失败")
	}
	return filepath.Join(filepath.Dir(file), "fixture")
}

func waitForAutomationSocket(t *testing.T, path string, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) net.Conn {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, err := dialAutomation(path)
		if err == nil {
			return conn
		}
		if cmd.ProcessState != nil {
			t.Fatalf("MyGo fixture 已退出：%v\\nstdout:\\n%s\\nstderr:\\n%s", cmd.ProcessState, stdout.String(), stderr.String())
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 MyGo 自动化 socket：%v\\nstdout:\\n%s\\nstderr:\\n%s", err, stdout.String(), stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForFixtureExit(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
		return
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		select {
		case <-waited:
		case <-time.After(2 * time.Second):
			t.Log("MyGo fixture 未在强制终止后退出")
		}
	}
}

func callAutomation(t *testing.T, client *automationClient, id int, method string, params any, out any) {
	t.Helper()
	_ = client.conn.SetDeadline(time.Now().Add(15 * time.Second))
	request := map[string]any{"id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	if err := client.enc.Encode(request); err != nil {
		t.Fatalf("发送 automation %s：%v", method, err)
	}
	line, err := client.dec.ReadBytes('\n')
	if err != nil {
		t.Fatalf("读取 automation %s：%v", method, err)
	}
	var response automationResponse
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("解析 automation %s：%v", method, err)
	}
	if response.Error != nil {
		t.Fatalf("automation %s：%s：%s", method, response.Error.Code, response.Error.Message)
	}
	if out != nil && len(response.Result) > 0 {
		if err := json.Unmarshal(response.Result, out); err != nil {
			t.Fatalf("解析 automation %s 结果：%v", method, err)
		}
	}
}

func assertAutomationNode(t *testing.T, nodes []struct {
	Key   any    `json:"key"`
	Label string `json:"label"`
}, key, label string) {
	t.Helper()
	for _, node := range nodes {
		if fmt.Sprint(node.Key) == key && node.Label == label {
			return
		}
	}
	t.Fatalf("automation tree 没有 key=%q label=%q：%+v", key, label, nodes)
}
