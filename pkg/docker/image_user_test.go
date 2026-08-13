package docker

import "testing"

func TestResolveImageUserIdentity(t *testing.T) {
	passwd := []byte("root:x:0:0:root:/root:/bin/sh\napp:x:1001:1002:app:/app:/bin/sh\nother:x:2000:2001:other:/app:/bin/sh\n")
	group := []byte("root:x:0:\napp:x:1002:\nshared:x:3000:\n")
	tests := []struct {
		name    string
		user    string
		passwd  []byte
		group   []byte
		wantUID uint32
		wantGID uint32
		wantErr bool
	}{
		{name: "empty defaults to root"},
		{name: "root works without account files", user: "root"},
		{name: "numeric pair works without account files", user: "4000:4001", wantUID: 4000, wantGID: 4001},
		{name: "numeric user uses passwd primary group", user: "2000", passwd: passwd, wantUID: 2000, wantGID: 2001},
		{name: "unknown numeric user defaults to root group", user: "4000", passwd: passwd, wantUID: 4000},
		{name: "named user uses passwd identity", user: "app", passwd: passwd, wantUID: 1001, wantGID: 1002},
		{name: "named user and group", user: "app:shared", passwd: passwd, group: group, wantUID: 1001, wantGID: 3000},
		{name: "numeric user and named group", user: "4000:shared", group: group, wantUID: 4000, wantGID: 3000},
		{name: "named user and numeric group", user: "app:4001", passwd: passwd, wantUID: 1001, wantGID: 4001},
		{name: "missing named user", user: "missing", passwd: passwd, wantErr: true},
		{name: "missing named group", user: "app:missing", passwd: passwd, group: group, wantErr: true},
		{name: "empty group", user: "app:", passwd: passwd, wantErr: true},
		{name: "too many separators", user: "app:shared:extra", passwd: passwd, group: group, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, gid, err := resolveImageUserIdentity(tt.user, tt.passwd, tt.group)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveImageUserIdentity(%q) error = %v, wantErr %t", tt.user, err, tt.wantErr)
			}
			if err == nil && (uid != tt.wantUID || gid != tt.wantGID) {
				t.Fatalf("resolveImageUserIdentity(%q) = %d:%d, want %d:%d", tt.user, uid, gid, tt.wantUID, tt.wantGID)
			}
		})
	}
}

func TestDirectNumericIdentity(t *testing.T) {
	identity, ok := directNumericIdentity("1001:1002")
	if !ok || identity.uid != 1001 || identity.gid != 1002 {
		t.Fatalf("directNumericIdentity() = %#v, %t", identity, ok)
	}
	if _, ok := directNumericIdentity("app:1002"); ok {
		t.Fatal("named user must require account resolution")
	}
}
