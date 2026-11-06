package server

import "testing"

func TestTagPath(t *testing.T) {
	if got := tagPath("dev/заметки"); got != "/tags/dev/%D0%B7%D0%B0%D0%BC%D0%B5%D1%82%D0%BA%D0%B8" {
		t.Errorf("tagPath = %s", got)
	}
}
