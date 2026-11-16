package agent

import "testing"

func TestSortProcessesByName(t *testing.T) {
	cases := []struct {
		name string
		in   []ProcInfo
		want []int // PIDs in the expected order
	}{
		{
			name: "case-insensitive, executable base name, arguments ignored, PID breaks ties",
			in: []ProcInfo{
				{PID: 5, Cmd: "sshd -D"},
				{PID: 3, Cmd: "/usr/bin/Zsh"},
				{PID: 9, Cmd: "bash --login"},
				{PID: 2, Cmd: "/usr/sbin/bash"},
				{PID: 7, Cmd: "Apache2"},
			},
			want: []int{7, 2, 9, 5, 3},
		},
		{
			name: "kernel thread with no arguments",
			in:   []ProcInfo{{PID: 4, Cmd: "kworker/0:1"}, {PID: 1, Cmd: "init"}},
			want: []int{1, 4},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sortProcesses(tc.in, "name")
			for i, p := range tc.in {
				if p.PID != tc.want[i] {
					t.Fatalf("position %d: got PID %d (%q), want PID %d", i, p.PID, p.Cmd, tc.want[i])
				}
			}
		})
	}
}
