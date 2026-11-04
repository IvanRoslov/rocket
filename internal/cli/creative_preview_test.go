package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/preview"
)

type previewStub struct {
	method, path string
	in           any
}

func (s *previewStub) Get(p string, in, out any) error {
	s.method = "GET"
	s.path = p
	if p == "/v1/creative/previews/setup" {
		*out.(*preview.Setup) = preview.Setup{AllowedPorts: []int{5100}}
	} else {
		*out.(*[]preview.Access) = []preview.Access{}
	}
	return nil
}
func (s *previewStub) Post(p string, in, out any) error {
	s.method = "POST"
	s.path = p
	s.in = in
	*out.(*preview.Access) = preview.Access{BasePath: "/p/token/", Status: "unavailable"}
	return nil
}
func (s *previewStub) Delete(p string, in, out any) error {
	s.method = "DELETE"
	s.path = p
	return nil
}
func TestCreativePreviewCLI(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		method, path string
	}{
		{[]string{"setup"}, "GET", "/v1/creative/previews/setup"},
		{[]string{"register", "--run", "1", "--stage", "2", "--item", "3", "--port", "5100"}, "POST", "/v1/creative/previews"},
		{[]string{"list", "--run", "1", "--stage", "2"}, "GET", "/v1/creative/previews?run_id=1&stage_id=2"},
		{[]string{"revoke", "abc"}, "DELETE", "/v1/creative/previews/abc"},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			s := &previewStub{}
			cmd := newCreativePreviewCmd(func() (creativePreviewClient, error) { return s, nil })
			var b bytes.Buffer
			cmd.SetOut(&b)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if s.method != tc.method || s.path != tc.path {
				t.Fatalf("%s %s", s.method, s.path)
			}
			if tc.args[0] == "register" {
				v, _ := json.Marshal(s.in)
				if string(v) != `{"run_id":1,"stage_id":2,"item_id":3,"port":5100}` || !strings.Contains(b.String(), "/p/token/") {
					t.Fatalf("%s %s", v, b.String())
				}
			}
		})
	}
}
func TestCreativePreviewCLIRejectsMissingContext(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"register", "--run", "1"}, {"register", "--run", "-1", "--port", "5100"}, {"revoke"}} {
		called := false
		cmd := newCreativePreviewCmd(func() (creativePreviewClient, error) { called = true; return &previewStub{}, nil })
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || called {
			t.Fatalf("args=%v dial=%v err=%v", args, called, err)
		}
	}
}
