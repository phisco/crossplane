package schema

import "testing"

func TestIsKnownJSONType(t *testing.T) {
	type args struct {
		t string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Known",
			args: args{t: "string"},
			want: true,
		},
		{
			name: "Unknown",
			args: args{t: "foo"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsKnownJSONType(tt.args.t); got != tt.want {
				t.Errorf("IsKnownJSONType() = %v, want %v", got, tt.want)
			}
		})
	}
}
