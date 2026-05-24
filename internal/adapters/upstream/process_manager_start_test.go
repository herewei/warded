package upstream

import "testing"

func TestSplitNodeArgv(t *testing.T) {
	command := "node '/Users/ivan/Developer/gitea.hanwei.cc/warded/warded_e2e/fixtures/upstream/managed-upstream.js' --host 127.0.0.1 --port 65200 --pid-file /tmp/u.pid --started-file /tmp/u-started.json"
	argv, ok := splitNodeArgv(command)
	if !ok {
		t.Fatal("expected ok")
	}
	if argv[0] != "node" {
		t.Fatalf("argv[0]=%q", argv[0])
	}
	if len(argv) < 4 {
		t.Fatalf("argv too short: %v", argv)
	}
}
