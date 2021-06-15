package images

import (
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/util/jsonpath"
	"reflect"
	"testing"
)

var (
	jsona = []byte(`{
  "flatImage": "name/of/image:with-tag",
  "deeply": {
    "nested": {
      "image": "another/image/name:with-a-tag",
      "set": [
        { "image": "first/in/list:123" },
        { "image": "second/in:456" }
      ]
    }
  }
}`)
)

func Test_parseJSONPath(t *testing.T) {
	var data interface{}
	if err := json.Unmarshal(jsona, &data); err != nil {
		t.Errorf("failed to unmarshal test article, %v", err)
	}

	j := jsonpath.New("")

	type args struct {
		input    interface{}
		name     string
		template string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name: "should find flat path with string result",
			args: args{
				input:    data,
				name:     "wut",
				template: "{.flatImage}",
			},
			want: []string{"name/of/image:with-tag"},
		},
		{
			name: "should find nested path with string result",
			args: args{
				input:    data,
				name:     "wut",
				template: "{.deeply.nested.image}",
			},
			want: []string{"another/image/name:with-a-tag"},
		},
		{
			name: "should find nested path with slice result",
			args: args{
				input:    data,
				name:     "wut",
				template: "{.deeply.nested.set[*].image}",
			},
			want: []string{"first/in/list:123", "second/in:456"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJSONPath(tt.args.input, j, tt.args.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSONPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseJSONPath() got = %v, want %v", got, tt.want)
			}
		})
	}
}
