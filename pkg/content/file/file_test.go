package file

// func TestNewFile(t *testing.T) {
// 	ctx := context.Background()
//
// 	tmpdir, err := os.MkdirTemp("", "hauler")
// 	if err != nil {
// 		t.Error(err)
// 	}
// 	defer os.Remove(tmpdir)
//
// 	// Make some temp files
// 	f, err := os.CreateTemp(tmpdir, "tmp")
// 	f.Write([]byte("content"))
// 	defer f.Close()
//
// 	c, err := cache.NewBoltDB(tmpdir, "cache")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	_ = c
//
// 	s := rstore.NewStore(ctx, tmpdir)
// 	s.Start()
// 	defer s.Stop()
//
// 	type args struct {
// 		cfg  v1alpha1.File
// 		root string
// 	}
// 	tests := []struct {
// 		name    string
// 		args    args
// 		want    *File
// 		wantErr bool
// 	}{
// 		{
// 			name: "should work",
// 			args: args{
// 				root: tmpdir,
// 				cfg: v1alpha1.File{
// 					Name: "myfile",
// 				},
// 			},
// 			want:    nil,
// 			wantErr: false,
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			got := New(tt.args.cfg, tt.args.root)
//
// 			ref, _ := name.ParseReference(path.Join("hauler", tt.args.cfg.Name))
// 			if err := s.Add(ctx, got, ref); err != nil {
// 				t.Error(err)
// 			}
//
// 			if !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("New() got = %v, want %v", got, tt.want)
// 			}
// 		})
// 	}
// }
