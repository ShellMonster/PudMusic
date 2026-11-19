package bili

import (
	"net/http"
	"testing"
)

// TestCommentWritesKeepTheReplyTarget verifies that replies and likes use the selected comment and video.
func TestCommentWritesKeepTheReplyTarget(t *testing.T) {
	fixture := newFakeBili(t)
	for _, test := range []struct {
		path string
		form map[string]string
	}{
		{"/x/v2/reply/add", map[string]string{"root": "20", "parent": "21", "message": "测试回复"}},
		{"/x/v2/reply/action", map[string]string{"rpid": "21", "action": "0"}},
	} {
		fixture.on(test.path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.PostForm.Get("type") != "1" || r.PostForm.Get("oid") != "77" {
				t.Errorf("unexpected comment target: %s %v", r.Method, r.PostForm)
			}
			for key, value := range test.form {
				if got := r.PostForm.Get(key); got != value {
					t.Errorf("%s = %q, want %q", key, got, value)
				}
			}
			writeJSON(w, jsonData(t, 0, "0", nil))
		})
	}
	client := fixture.client(t, Options{})
	if err := client.AddComment(t.Context(), CommentTypeVideo, 77, 20, 21, " 测试回复 "); err != nil {
		t.Fatal(err)
	}
	if err := client.LikeComment(t.Context(), CommentTypeVideo, 77, 21, false); err != nil {
		t.Fatal(err)
	}
}
