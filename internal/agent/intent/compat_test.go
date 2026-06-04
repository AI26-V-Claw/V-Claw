package intent

import "testing"

func TestMapSystemOp_ShellHeuristics(t *testing.T) {
	cases := []struct {
		name string
		call ToolCallInfo
		want SystemOpType
	}{
		{
			name: "delete shell",
			call: ToolCallInfo{Name: "sandbox.runShell", Parameters: map[string]interface{}{"command": "xóa file cũ"}},
			want: SystemOpDelete,
		},
		{
			name: "write shell",
			call: ToolCallInfo{Name: "sandbox.runShell", Parameters: map[string]interface{}{"command": "ghi file báo cáo tổng kết"}},
			want: SystemOpWrite,
		},
		{
			name: "shell shell",
			call: ToolCallInfo{Name: "sandbox.runShell", Parameters: map[string]interface{}{"command": "mở shell và kiểm tra thư mục"}},
			want: SystemOpShell,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapSystemOp([]ToolCallInfo{tc.call}); got != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, got)
			}
		})
	}
}
