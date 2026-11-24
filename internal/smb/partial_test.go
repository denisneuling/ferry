package smb

import "testing"

// One step at a time per upload: a second at the same time is busy, and
// the upload is free again when the first is done.
func TestHold(t *testing.T) {
	release, err := hold("alex\x00a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hold("alex\x00a"); KindOf(err) != KindBusy {
		t.Fatalf("a second step at the same time: %v", err)
	}
	other, err := hold("alex\x00b")
	if err != nil {
		t.Fatalf("another upload: %v", err)
	}
	other()
	release()
	again, err := hold("alex\x00a")
	if err != nil {
		t.Fatalf("after the first: %v", err)
	}
	again()
}
