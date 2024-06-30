package content

import "testing"

func Test_splitImageRef(t *testing.T) {
	type args struct {
		ref string
	}
	tests := []struct {
		name          string
		args          args
		wantedBaseRef string
		wantedHash    string
	}{
		// TODO: Add test cases.
		{
			"Split ref digest",
			args{"sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064-library/nginx@sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064-dev.cosignproject.cosign/imageIndex@sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064"},
			"sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064-library/nginx@sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064-dev.cosignproject.cosign/imageIndex",
			"sha256:95b01e2e9ab0702ce2f1a8f05f90e6408fd1f4b5e5006c6088ba5a864ed42064",
		}, {
			"Split ref no digest",
			args{"sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0-library/alpine:latest-dev.cosignproject.cosign/imageIndex@sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0"},
			"sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0-library/alpine:latest-dev.cosignproject.cosign/imageIndex",
			"sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseRef, hash := splitImageRef(tt.args.ref)
			if baseRef != tt.wantedBaseRef {
				// t.Errorf("splitImageReg() ref = %v", tt.args.ref)
				t.Errorf("splitImageRef() got baseRef = %v,\n"+
					"			 				wanted baseRef = %v", baseRef, tt.wantedBaseRef)
			}
			if hash != tt.wantedHash {
				// t.Errorf("splitImageReg() ref = %v", tt.args.ref)
				t.Errorf("splitImageRef() got hash = %v,\n"+
					"			 				wanted hash = %v", hash, tt.wantedHash)
			}
		})
	}
}
