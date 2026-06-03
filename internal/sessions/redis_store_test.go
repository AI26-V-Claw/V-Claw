package sessions

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseRedisURL(t *testing.T) {
	address, username, password, db, err := parseRedisURL("redis://user:pass@localhost:6380/2")
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	if address != "localhost:6380" || username != "user" || password != "pass" || db != "2" {
		t.Fatalf("unexpected parse result address=%q username=%q password=%q db=%q", address, username, password, db)
	}
}

func TestParseRedisURLDefaultsPortAndPasswordOnly(t *testing.T) {
	address, username, password, db, err := parseRedisURL("redis://:secret@localhost/0")
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	if address != "localhost:6379" || username != "" || password != "secret" || db != "0" {
		t.Fatalf("unexpected parse result address=%q username=%q password=%q db=%q", address, username, password, db)
	}
}

func TestEncodeRedisCommand(t *testing.T) {
	got := encodeRedisCommand([]string{"SET", "key", "value"})
	want := "*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n"
	if got != want {
		t.Fatalf("unexpected command encoding:\nwant %q\ngot  %q", want, got)
	}
}

func TestReadRedisArrayReply(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("*2\r\n$5\r\nhello\r\n$5\r\nworld\r\n"))
	reply, err := readRedisReply(reader)
	if err != nil {
		t.Fatalf("read redis reply: %v", err)
	}
	items, ok := reply.([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("unexpected reply %#v", reply)
	}
	if string(items[0].([]byte)) != "hello" || string(items[1].([]byte)) != "world" {
		t.Fatalf("unexpected array items %#v", items)
	}
}
