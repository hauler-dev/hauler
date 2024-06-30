package content

import "testing"

func Test_splitImageRef(t *testing.T) {
	type args struct {
		ref string
	}
	tests := []struct {
		name  string
		args  args
		want  string
		want1 string
	}{
		// TODO: Add test cases.
		{"Split digest",
			args{"sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064-library/nginx@sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064-dev.cosignproject.cosign/imageIndex@sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064"},
			"sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064-library/nginx@sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064-dev.cosignproject.cosign/imageIndex",
			"sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064",
		}, {
			"split no digest",
			args{"sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0-library/alpine:latest-dev.cosignproject.cosign/imageIndex@sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0"},
			"sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0-library/alpine:latest-dev.cosignproject.cosign/imageIndex",
			"sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := splitImageRef(tt.args.ref)
			if got != tt.want {
				//t.Errorf("splitImageReg() ref = %v", tt.args.ref)
				t.Errorf("splitImageRef() got  = %v,\n"+
					"			 				want = %v", got, tt.want)
			}
			if got1 != tt.want1 {
				//t.Errorf("splitImageReg() ref = %v", tt.args.ref)
				t.Errorf("splitImageRef() got1 = %v,\n"+
					"			 				want = %v", got1, tt.want1)
			}
		})
	}
}
