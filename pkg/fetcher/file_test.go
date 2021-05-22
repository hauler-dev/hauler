package fetcher

import "testing"

func TestGetFileNameFromURL(t *testing.T) {
	type args struct {
		furl string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "shouldn't need extension",
			args: args{
				furl: "https://github.com/k3s-io/k3s/releases/download/v1.21.1%2Bk3s1/k3s",
			},
			want: "k3s",
		},
		{
			name: "should work with extension",
			args: args{
				furl: "https://github.com/k3s-io/k3s/releases/download/v1.21.1%2Bk3s1/k3s-airgap-images-arm.tar.zst",
			},
			want: "k3s-airgap-images-arm.tar.zst",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetFileNameFromURL(tt.args.furl); got != tt.want {
				t.Errorf("GetFileNameFromURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
