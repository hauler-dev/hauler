package download

import (
	"context"
	"testing"
)

func TestCmd(t *testing.T) {
	ctx := context.Background()

	type args struct {
		ctx       context.Context
		o         *Opts
		reference string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "should work",
			args: args{
				ctx:       ctx,
				o:         &Opts{DestinationDir: ""},
				reference: "localhost:3000/hauler/file.txt:latest",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Cmd(tt.args.ctx, tt.args.o, tt.args.reference); (err != nil) != tt.wantErr {
				t.Errorf("Cmd() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
