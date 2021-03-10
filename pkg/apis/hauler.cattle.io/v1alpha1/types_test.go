package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TODO - should these tests be kept?
// They formalize some low-level behavior of conversion between files and data
// structures, but seem like possible overhead for changing any types.

func TestPackageDefType_UnmarshalJSON(t *testing.T) {
	type testCase struct {
		Input        string
		Expected     PackageType
		ExpectedFail bool
	}
	cases := []testCase{
		{
			Input:    `"K3s"`,
			Expected: PackageTypeK3s,
		},
		{
			Input:    `"ContainerImages"`,
			Expected: PackageTypeContainerImages,
		},
		{
			Input:    `"GitRepository"`,
			Expected: PackageTypeGitRepository,
		},
		{
			Input:    `"FileTree"`,
			Expected: PackageTypeFileTree,
		},
		{
			Input:    `"Aaa"`,
			Expected: PackageTypeUnknown,
		},
		{
			Input:    `""`,
			Expected: PackageTypeUnknown,
		},
		{
			Input:    `null`,
			Expected: PackageTypeUnknown,
		},
		{
			Input:        `0`,
			ExpectedFail: true,
		},
		{
			Input:        `{}`,
			ExpectedFail: true,
		},
	}

	for _, curCase := range cases {
		var actual PackageType
		actualErr := json.Unmarshal([]byte(curCase.Input), &actual)

		switch {
		case curCase.ExpectedFail:
			if actualErr == nil {
				t.Errorf("input %q\nexpected failure, instead got value %q", curCase.Input, actual)
			}
		case actualErr != nil:
			t.Errorf("input %q\nunexpected error: %v", curCase.Input, actualErr)
		case actual != curCase.Expected:
			t.Errorf("input %q\nbad result:\nexpected %q\ngot      %q", curCase.Input, curCase.Expected, actual)
		default:
			// test passed
		}
	}
}

func TestPackageDefType_MarshalJSON(t *testing.T) {
	type testCase struct {
		Input        PackageType
		Expected     string
		ExpectedFail bool
	}
	cases := []testCase{
		{
			Input:    PackageTypeK3s,
			Expected: `"K3s"`,
		},
		{
			Input:    PackageTypeContainerImages,
			Expected: `"ContainerImages"`,
		},
		{
			Input:    PackageTypeGitRepository,
			Expected: `"GitRepository"`,
		},
		{
			Input:    PackageTypeFileTree,
			Expected: `"FileTree"`,
		},
		{
			Input:    PackageTypeUnknown,
			Expected: `""`,
		},
		{
			Input:    PackageType("Aaa"),
			Expected: `""`,
		},
	}

	for _, curCase := range cases {
		actual, actualErr := json.Marshal(curCase.Input)
		switch {
		case curCase.ExpectedFail:
			if actualErr == nil {
				t.Errorf("input %q\nexpected failure, instead got value %q", curCase.Input, actual)
			}
		case actualErr != nil:
			t.Errorf("input %q\nunexpected error: %v", curCase.Input, actualErr)
		case string(actual) != curCase.Expected:
			t.Errorf("input %q\nbad result:\nexpected %q\ngot      %q", curCase.Input, curCase.Expected, actual)
		default:
			// test passed
		}
	}
}

func TestPackageDef_UnmarshalJSON(t *testing.T) {
	type testCase struct {
		Input        string
		Expected     Package
		ExpectedFail bool
	}
	cases := []testCase{
		{
			Input: `{}`,
			Expected: Package{
				Type: PackageTypeUnknown,
			},
		},
		{
			Input: `{"type":"K3s"}`,
			Expected: Package{
				Type: PackageTypeK3s,
				k3s:  &PackageK3s{},
			},
		},
		{
			Input: `{"type":"ContainerImages"}`,
			Expected: Package{
				Type:            PackageTypeContainerImages,
				containerImages: &PackageContainerImages{},
			},
		},
		{
			Input: `{"type":"GitRepository"}`,
			Expected: Package{
				Type:          PackageTypeGitRepository,
				gitRepository: &PackageGitRepository{},
			},
		},
		{
			Input: `{"type":"FileTree"}`,
			Expected: Package{
				Type:     PackageTypeFileTree,
				fileTree: &PackageFileTree{},
			},
		},
		{
			Input: `{"name":"hauler-k3s-a","type":"K3s"}`,
			Expected: Package{
				Name: "hauler-k3s-a",
				Type: PackageTypeK3s,
				k3s:  &PackageK3s{},
			},
		},
		{
			Input: `{"name":"hauler-k3s-b","type":"K3s","release":"1.19.5+k3s1"}`,
			Expected: Package{
				Name: "hauler-k3s-b",
				Type: PackageTypeK3s,
				k3s: &PackageK3s{
					Release: "1.19.5+k3s1",
				},
			},
		},
		{
			Input: `{"name":"hauler-k3s-c","type":"K3s","release":"1.19.5+k3s1","installScriptRef":"release-1.19"}`,
			Expected: Package{
				Name: "hauler-k3s-c",
				Type: PackageTypeK3s,
				k3s: &PackageK3s{
					Release:          "1.19.5+k3s1",
					InstallScriptRef: "release-1.19",
				},
			},
		},
		{
			Input: `{"name":"hauler-k3s-d","type":"K3s","release":"1.19.5+k3s1","installScriptRef":"release-1.19","unknown":"value"}`,
			Expected: Package{
				Name: "hauler-k3s-d",
				Type: PackageTypeK3s,
				k3s: &PackageK3s{
					Release:          "1.19.5+k3s1",
					InstallScriptRef: "release-1.19",
				},
			},
		},
		{
			Input:        `{"name":"hauler-k3s-fail-a","type":"K3s","release":1.19,"installScriptRef":"release-1.19"}`,
			ExpectedFail: true,
		},
		{
			Input:        `{"name":"hauler-k3s-fail-b","type":"K3s","release":"1.19","installScriptRef":false}`,
			ExpectedFail: true,
		},
		{
			Input: `{"name":"hauler-images-a","type":"ContainerImages","imageLists":["file:///home/user/images.txt","https://my.example.com/containers/image-list.txt"]}`,
			Expected: Package{
				Name: "hauler-images-a",
				Type: PackageTypeContainerImages,
				containerImages: &PackageContainerImages{
					ImageLists: []string{
						"file:///home/user/images.txt",
						"https://my.example.com/containers/image-list.txt",
					},
				},
			},
		},
		{
			Input: `{"name":"hauler-images-b","type":"ContainerImages","imageArchives":["file:///home/user/image-archive.tar","https://my.example.com/containers/image-archive.tar.gz"]}`,
			Expected: Package{
				Name: "hauler-images-b",
				Type: PackageTypeContainerImages,
				containerImages: &PackageContainerImages{
					ImageArchives: []string{
						"file:///home/user/image-archive.tar",
						"https://my.example.com/containers/image-archive.tar.gz",
					},
				},
			},
		},
		{
			Input: `{"name":"hauler-images-c","type":"ContainerImages","imageLists":["file:///home/user/images.txt","https://my.example.com/containers/image-list.txt"],"imageArchives":["file:///home/user/image-archive.tar","https://my.example.com/containers/image-archive.tar.gz"]}`,
			Expected: Package{
				Name: "hauler-images-c",
				Type: PackageTypeContainerImages,
				containerImages: &PackageContainerImages{
					ImageLists: []string{
						"file:///home/user/images.txt",
						"https://my.example.com/containers/image-list.txt",
					},
					ImageArchives: []string{
						"file:///home/user/image-archive.tar",
						"https://my.example.com/containers/image-archive.tar.gz",
					},
				},
			},
		},
		{
			Input: `{"name":"hauler-images-d","type":"ContainerImages","imageLists":["file:///home/user/images.txt","https://my.example.com/containers/image-list.txt"],"imageArchives":["file:///home/user/image-archive.tar","https://my.example.com/containers/image-archive.tar.gz"],"unknown":123}`,
			Expected: Package{
				Name: "hauler-images-d",
				Type: PackageTypeContainerImages,
				containerImages: &PackageContainerImages{
					ImageLists: []string{
						"file:///home/user/images.txt",
						"https://my.example.com/containers/image-list.txt",
					},
					ImageArchives: []string{
						"file:///home/user/image-archive.tar",
						"https://my.example.com/containers/image-archive.tar.gz",
					},
				},
			},
		},
		{
			Input: `{"name":"hauler-git-a","type":"GitRepository","repository":"https://github.com/rancherfederal/hauler"}`,
			Expected: Package{
				Name: "hauler-git-a",
				Type: PackageTypeGitRepository,
				gitRepository: &PackageGitRepository{
					Repository: "https://github.com/rancherfederal/hauler",
				},
			},
		},
		{
			Input: `{"name":"hauler-git-b","type":"GitRepository","repository":"https://github.com/rancherfederal/hauler","httpsUsernameEnvVar":"HAULER_USER","httpsPasswordEnvVar":"HAULER_PASSWORD"}`,
			Expected: Package{
				Name: "hauler-git-b",
				Type: PackageTypeGitRepository,
				gitRepository: &PackageGitRepository{
					Repository:          "https://github.com/rancherfederal/hauler",
					HTTPSUsernameEnvVar: "HAULER_USER",
					HTTPSPasswordEnvVar: "HAULER_PASSWORD",
				},
			},
		},
		{
			Input: `{"name":"hauler-git-c","type":"GitRepository","repository":"git@github.com:rancherfederal/hauler.git","sshPrivateKeyPath":"~/.ssh/id_rsa_hauler"}`,
			Expected: Package{
				Name: "hauler-git-c",
				Type: PackageTypeGitRepository,
				gitRepository: &PackageGitRepository{
					Repository:        "git@github.com:rancherfederal/hauler.git",
					SSHPrivateKeyPath: "~/.ssh/id_rsa_hauler",
				},
			},
		},
		{
			Input: `{"name":"hauler-git-d","type":"GitRepository","repository":"git@github.com:rancherfederal/hauler.git","sshPrivateKeyPath":"~/.ssh/id_rsa_hauler","newField":true}`,
			Expected: Package{
				Name: "hauler-git-d",
				Type: PackageTypeGitRepository,
				gitRepository: &PackageGitRepository{
					Repository:        "git@github.com:rancherfederal/hauler.git",
					SSHPrivateKeyPath: "~/.ssh/id_rsa_hauler",
				},
			},
		},
		{
			Input: `{"name":"hauler-files-a","type":"FileTree","sourceBasePath":"file:///home/user/base-dir","servingBasePath":"/dest-dir"}`,
			Expected: Package{
				Name: "hauler-files-a",
				Type: PackageTypeFileTree,
				fileTree: &PackageFileTree{
					SourceBasePath:  "file:///home/user/base-dir",
					ServingBasePath: "/dest-dir",
				},
			},
		},
		{
			Input: `{"name":"hauler-files-b","type":"FileTree","sourceBasePath":"file:///home/user/base-dir","servingBasePath":"/dest-dir","nullField":null}`,
			Expected: Package{
				Name: "hauler-files-b",
				Type: PackageTypeFileTree,
				fileTree: &PackageFileTree{
					SourceBasePath:  "file:///home/user/base-dir",
					ServingBasePath: "/dest-dir",
				},
			},
		},
	}

	for _, curCase := range cases {
		var actual Package
		actualErr := json.Unmarshal([]byte(curCase.Input), &actual)
		switch {
		case curCase.ExpectedFail:
			if actualErr == nil {
				t.Errorf("input:\n%s\nexpected failure, instead got value %q", curCase.Input, actual)
			}
		case actualErr != nil:
			t.Errorf("input:\n%s\nunexpected error: %v", curCase.Input, actualErr)
		case !reflect.DeepEqual(actual, curCase.Expected):
			t.Errorf("input:\n%s\nbad result:\nexpected %s\ngot      %s", curCase.Input, curCase.Expected, actual)
		default:
			// test passed
		}
	}
}

func TestPackageDef_MarshalJSON(t *testing.T) {
	type testCase struct {
		Input        Package
		Expected     string
		ExpectedFail bool
	}
	cases := []testCase{
		{
			Input: Package{
				Type: PackageTypeUnknown,
			},
			Expected: `{}`,
		},
		{
			Input: Package{
				Name: "hauler-unknown-a",
				Type: PackageTypeUnknown,
			},
			Expected: `{"name":"hauler-unknown-a"}`,
		},
		{
			Input: Package{
				Type: PackageTypeK3s,
			},
			Expected: `{"type":"K3s"}`,
		},
		{
			Input: Package{
				Type: PackageTypeContainerImages,
			},
			Expected: `{"type":"ContainerImages"}`,
		},
		{
			Input: Package{
				Type: PackageTypeGitRepository,
			},
			Expected: `{"type":"GitRepository"}`,
		},
		{
			Input: Package{
				Type: PackageTypeFileTree,
			},
			Expected: `{"type":"FileTree"}`,
		},
		{
			Input: Package{
				Name: "hauler-k3s-a",
				Type: PackageTypeK3s,
			},
			Expected: `{"name":"hauler-k3s-a","type":"K3s"}`,
		},
		{
			Input: Package{
				Name: "hauler-k3s-b",
				Type: PackageTypeK3s,
				k3s: &PackageK3s{
					Release:          "1.19.5+k3s1",
					InstallScriptRef: "release-1.19",
				},
			},
			Expected: `{"name":"hauler-k3s-b","type":"K3s","release":"1.19.5+k3s1","installScriptRef":"release-1.19"}`,
		},
		{
			Input: Package{
				Name: "hauler-git-a",
				Type: PackageTypeGitRepository,
			},
			Expected: `{"name":"hauler-git-a","type":"GitRepository"}`,
		},
		{
			Input: Package{
				Name: "hauler-git-b",
				Type: PackageTypeGitRepository,
				gitRepository: &PackageGitRepository{
					Repository:          "https://github.com/rancherfederal/hauler",
					HTTPSUsernameEnvVar: "HAULER_USER",
					HTTPSPasswordEnvVar: "HAULER_PASSWORD",
				},
			},
			Expected: `{"name":"hauler-git-b","type":"GitRepository","repository":"https://github.com/rancherfederal/hauler","httpsUsernameEnvVar":"HAULER_USER","httpsPasswordEnvVar":"HAULER_PASSWORD"}`,
		},
		{
			Input: Package{
				Name: "hauler-git-c",
				Type: PackageTypeGitRepository,
				gitRepository: &PackageGitRepository{
					Repository:        "git@github.com:rancherfederal/hauler.git",
					SSHPrivateKeyPath: "~/.ssh/id_rsa_hauler",
				},
			},
			Expected: `{"name":"hauler-git-c","type":"GitRepository","repository":"git@github.com:rancherfederal/hauler.git","sshPrivateKeyPath":"~/.ssh/id_rsa_hauler"}`,
		},
	}

	for _, curCase := range cases {
		actual, actualErr := json.Marshal(curCase.Input)
		switch {
		case curCase.ExpectedFail:
			if actualErr == nil {
				t.Errorf("input:\n%s\nexpected failure, instead got value %s", curCase.Input, actual)
			}
		case actualErr != nil:
			t.Errorf("input:\n%s\nunexpected error: %v", curCase.Input, actualErr)
		case string(actual) != curCase.Expected:
			t.Errorf("input:\n%s\nbad result:\nexpected %s\ngot      %s", curCase.Input, curCase.Expected, actual)
		default:
			// test passed
		}
	}
}
