package bili

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (c *Client) AddComment(ctx context.Context, kind, object, root, parent int64, message string) error {
	message = strings.TrimSpace(message)
	if message == "" || root < 0 || parent < 0 || (root == 0) != (parent == 0) {
		return errors.New("请输入有效的评论内容")
	}
	return c.writeComment(ctx, "/x/v2/reply/add", kind, object, url.Values{
		"message": {message}, "plat": {"1"},
		"root": {strconv.FormatInt(root, 10)}, "parent": {strconv.FormatInt(parent, 10)},
	})
}

func (c *Client) LikeComment(ctx context.Context, kind, object, reply int64, liked bool) error {
	if reply <= 0 {
		return errors.New("评论不存在")
	}
	action := "0"
	if liked {
		action = "1"
	}
	return c.writeComment(ctx, "/x/v2/reply/action", kind, object, url.Values{
		"rpid": {strconv.FormatInt(reply, 10)}, "action": {action},
	})
}

func (c *Client) writeComment(ctx context.Context, path string, kind, object int64, form url.Values) error {
	if object <= 0 || kind != CommentTypeVideo && kind != CommentTypeAudio {
		return errors.New("这首歌没有评论区")
	}
	form.Set("type", strconv.FormatInt(kind, 10))
	form.Set("oid", strconv.FormatInt(object, 10))
	_, err := c.Do(ctx, Call{Method: http.MethodPost, URL: c.hostAPI + path, Form: form, CSRF: true})
	return err
}
