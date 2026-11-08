package wodby1

import "testing"

func TestTargetCrontab(t *testing.T) {
	tests := []struct {
		source   string
		want     string
		adjusted bool
	}{
		{"* * * * *", "*/5 * * * *", true},
		{"*/2 4-20 * * *", "*/5 4-20 * * *", true},
		{"*/4 * * * 1-5", "*/5 * * * 1-5", true},
		{"*/7 * * * *", "*/5 * * * *", true}, // 56 then 0 of the next hour
		{"0,58 * * * *", "*/5 * * * *", true},
		{"0,58 0,23 * * *", "*/5 0,23 * * *", true},
		{"* * * * 7", "*/5 * * * 0", true},
		{"*/5 * * * *", "*/5 * * * *", false},
		{"*/15 * * * *", "*/15 * * * *", false},
		{"0,58 3 * * *", "0,58 3 * * *", false},
		{"5/10 * * * *", "5/10 * * * *", false},
		{"0 2 * * *", "0 2 * * *", false},
		{"0 2 * * 7", "0 2 * * 0", false},
		{"@hourly", "@hourly", false},
		{"0 0 * * * *", "0 0 * * * *", false},
		{"* JAN * * *", "* JAN * * *", false},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			got, adjusted := targetCrontab(tt.source)
			if got != tt.want || adjusted != tt.adjusted {
				t.Fatalf("targetCrontab(%q) = %q, %v; want %q, %v", tt.source, got, adjusted, tt.want, tt.adjusted)
			}
		})
	}
}

func TestCronFieldValues(t *testing.T) {
	values, ok := cronFieldValues("1-10/3,20,*/30", 0, 59)
	if !ok {
		t.Fatal("cronFieldValues() not ok")
	}
	want := map[int]bool{0: true, 1: true, 4: true, 7: true, 10: true, 20: true, 30: true}
	for value, set := range values {
		if set != want[value] {
			t.Fatalf("value %d set = %v, want %v", value, set, want[value])
		}
	}
	for _, field := range []string{"60", "5-1", "*/0", "a", "1-"} {
		if _, ok := cronFieldValues(field, 0, 59); ok {
			t.Fatalf("cronFieldValues(%q) ok, want not ok", field)
		}
	}
}
