package main

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v2"
)

var (
	upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	logMux   sync.Mutex
	logLines []string
	repos    = make(map[string]Repository)
	reposMux sync.RWMutex
	registries    = make(map[string]RegistryConfig)
	registriesMux sync.RWMutex
)

type Repository struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type RegistryConfig struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Insecure bool   `json:"insecure"`
}

type PushRequest struct {
	RegistryName string   `json:"registryName"`
	Content      []string `json:"content"`
}

type ChartInfo struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	AppVersion  string   `json:"appVersion"`
	Repository  string   `json:"repository"`
}

type HelmIndex struct {
	Entries map[string][]HelmChartVersion `yaml:"entries"`
}

type HelmChartVersion struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
	AppVersion  string `yaml:"appVersion"`
}

type ImageInfo struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type AddContentRequest struct {
	Type                string `json:"type"`
	Name                string `json:"name"`
	Version             string `json:"version"`
	Repository          string `json:"repository"`
	Platform            string `json:"platform"`
	AddImages           bool   `json:"addImages"`
	AddDependencies     bool   `json:"addDependencies"`
	Registry            string `json:"registry"`
	Key                 string `json:"key"`
	Rewrite             string `json:"rewrite"`
	Username            string `json:"username"`
	Password            string `json:"password"`
	InsecureSkipTLS     bool   `json:"insecureSkipTls"`
	KubeVersion         string `json:"kubeVersion"`
	Verify              bool   `json:"verify"`
	Values              string `json:"values"`
	CertIdentity        string `json:"certIdentity"`
	CertIdentityRegexp  string `json:"certIdentityRegexp"`
	CertOIDCIssuer      string `json:"certOidcIssuer"`
	CertOIDCIssuerRegexp string `json:"certOidcIssuerRegexp"`
	CertGithubWorkflow  string `json:"certGithubWorkflow"`
	UseTlogVerify       bool   `json:"useTlogVerify"`
}

type Response struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// safePath sanitizes a filename to prevent path traversal attacks.
// It extracts the base name and rejects empty or dot-prefixed results.
func safePath(baseDir, fileName string) (string, error) {
	clean := filepath.Base(fileName)
	if clean == "." || clean == ".." || clean == "" || clean == string(filepath.Separator) {
		return "", fmt.Errorf("invalid filename")
	}
	return filepath.Join(baseDir, clean), nil
}

func main() {
	loadRepositories()
	loadRegistries()
	
	r := mux.NewRouter()

	// Existing endpoints
	r.HandleFunc("/api/health", healthHandler).Methods("GET")
	r.HandleFunc("/api/store/info", storeInfoHandler).Methods("GET")
	r.HandleFunc("/api/store/sync", storeSyncHandler).Methods("POST")
	r.HandleFunc("/api/store/save", storeSaveHandler).Methods("POST")
	r.HandleFunc("/api/store/load", storeLoadHandler).Methods("POST")
	r.HandleFunc("/api/files/upload", fileUploadHandler).Methods("POST")
	r.HandleFunc("/api/files/list", fileListHandler).Methods("GET")
	r.HandleFunc("/api/files/download/{filename}", fileDownloadHandler).Methods("GET")
	r.HandleFunc("/api/cert/upload", certUploadHandler).Methods("POST")
	r.HandleFunc("/api/serve/start", serveStartHandler).Methods("POST")
	r.HandleFunc("/api/serve/stop", serveStopHandler).Methods("POST")
	r.HandleFunc("/api/serve/status", serveStatusHandler).Methods("GET")
	r.HandleFunc("/api/logs", logsHandler)

	// New endpoints for enhanced functionality
	r.HandleFunc("/api/repos/add", repoAddHandler).Methods("POST")
	r.HandleFunc("/api/repos/list", repoListHandler).Methods("GET")
	r.HandleFunc("/api/repos/remove/{name}", repoRemoveHandler).Methods("DELETE")
	r.HandleFunc("/api/repos/charts/{name}", repoChartsHandler).Methods("GET")
	r.HandleFunc("/api/charts/search", chartSearchHandler).Methods("GET")
	r.HandleFunc("/api/charts/info", chartInfoHandler).Methods("GET")
	r.HandleFunc("/api/images/search", imageSearchHandler).Methods("GET")
	r.HandleFunc("/api/store/add-content", addContentHandler).Methods("POST")
	r.HandleFunc("/api/files/delete/{filename}", fileDeleteHandler).Methods("DELETE")
	r.HandleFunc("/api/store/clear", storeClearHandler).Methods("POST")
	r.HandleFunc("/api/system/reset", systemResetHandler).Methods("POST")
	r.HandleFunc("/api/registry/configure", registryConfigureHandler).Methods("POST")
	r.HandleFunc("/api/registry/list", registryListHandler).Methods("GET")
	r.HandleFunc("/api/registry/remove/{name}", registryRemoveHandler).Methods("DELETE")
	r.HandleFunc("/api/registry/test", registryTestHandler).Methods("POST")
	r.HandleFunc("/api/registry/push", registryPushHandler).Methods("POST")
	r.HandleFunc("/api/store/add-file", storeAddFileHandler).Methods("POST")
	r.HandleFunc("/api/store/extract", storeExtractHandler).Methods("POST")
	r.HandleFunc("/api/store/artifacts", storeArtifactsHandler).Methods("GET")
	r.HandleFunc("/api/store/remove/{artifact:.*}", storeRemoveHandler).Methods("DELETE")
	r.HandleFunc("/api/registry/login", registryLoginHandler).Methods("POST")
	r.HandleFunc("/api/registry/logout", registryLogoutHandler).Methods("POST")
	r.HandleFunc("/api/key/upload", keyUploadHandler).Methods("POST")
	r.HandleFunc("/api/key/list", keyListHandler).Methods("GET")
	r.HandleFunc("/api/tlscert/upload", tlsCertUploadHandler).Methods("POST")
	r.HandleFunc("/api/tlscert/list", tlsCertListHandler).Methods("GET")
	r.HandleFunc("/api/values/upload", valuesUploadHandler).Methods("POST")
	r.HandleFunc("/api/values/list", valuesListHandler).Methods("GET")

	fs := http.FileServer(http.Dir("/app/frontend"))
	r.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Content-Type", "application/javascript")
		} else if strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Content-Type", "text/css")
		}
		fs.ServeHTTP(w, r)
	}))

	log.Println("Starting Hauler UI on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

func loadRepositories() {
	repoFile := "/data/config/repositories.json"
	data, err := os.ReadFile(repoFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, &repos)
}

func saveRepositories() error {
	repoFile := "/data/config/repositories.json"
	os.MkdirAll(filepath.Dir(repoFile), 0755)
	data, err := json.Marshal(repos)
	if err != nil {
		return err
	}
	return os.WriteFile(repoFile, data, 0644)
}

func repoAddHandler(w http.ResponseWriter, r *http.Request) {
	var repo Repository
	if err := json.NewDecoder(r.Body).Decode(&repo); err != nil {
		respondError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	reposMux.Lock()
	repos[repo.Name] = repo
	reposMux.Unlock()

	if err := saveRepositories(); err != nil {
		respondError(w, "Failed to save repository", http.StatusInternalServerError)
		return
	}

	respondJSON(w, Response{Success: true, Output: "Repository added successfully"})
}

func repoListHandler(w http.ResponseWriter, r *http.Request) {
	reposMux.RLock()
	defer reposMux.RUnlock()

	repoList := make([]Repository, 0, len(repos))
	for _, repo := range repos {
		repoList = append(repoList, repo)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"repositories": repoList})
}

func repoRemoveHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	reposMux.Lock()
	delete(repos, name)
	reposMux.Unlock()

	if err := saveRepositories(); err != nil {
		respondError(w, "Failed to save repositories", http.StatusInternalServerError)
		return
	}

	respondJSON(w, Response{Success: true, Output: "Repository removed successfully"})
}

func repoChartsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	reposMux.RLock()
	repo, exists := repos[name]
	reposMux.RUnlock()

	if !exists {
		respondError(w, "Repository not found", http.StatusNotFound)
		return
	}

	// Check if this is an OCI registry
	if strings.HasPrefix(repo.URL, "oci://") {
		// OCI registries don't have browsable indexes
		// Return empty response with helpful message
		json.NewEncoder(w).Encode(map[string]interface{}{
			"charts":  map[string][]string{},
			"details": map[string]ChartInfo{},
			"isOCI":   true,
			"message": "OCI registries cannot be browsed. Please use 'Add Chart Directly' tab and specify the chart name manually.",
		})
		return
	}

	indexURL := strings.TrimSuffix(repo.URL, "/") + "/index.yaml"
	resp, err := http.Get(indexURL)
	if err != nil {
		respondError(w, "Failed to fetch repository index", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respondError(w, "Repository index not found", http.StatusNotFound)
		return
	}

	body, _ := io.ReadAll(resp.Body)
	var index HelmIndex
	if err := yaml.Unmarshal(body, &index); err != nil {
		respondError(w, "Failed to parse repository index", http.StatusInternalServerError)
		return
	}

	charts := make(map[string][]string)
	chartDetails := make(map[string]ChartInfo)

	for chartName, versions := range index.Entries {
		if len(versions) > 0 {
			versionList := make([]string, len(versions))
			for i, v := range versions {
				versionList[i] = v.Version
			}
			charts[chartName] = versionList
			chartDetails[chartName] = ChartInfo{
				Name:        versions[0].Name,
				Version:     versions[0].Version,
				Description: versions[0].Description,
				AppVersion:  versions[0].AppVersion,
				Repository:  repo.URL,
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"charts":  charts,
		"details": chartDetails,
		"isOCI":   false,
	})
}

func chartSearchHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	repo := r.URL.Query().Get("repo")

	var charts []ChartInfo

	reposMux.RLock()
	defer reposMux.RUnlock()

	// Return placeholder for now - requires Helm repo index parsing
	// Hauler uses Helm Go libraries, not CLI
	// Charts are added directly via hauler store add chart command
	for _, repository := range repos {
		if repo != "" && repository.Name != repo {
			continue
		}
		
		// Placeholder chart data
		if query == "" || strings.Contains(strings.ToLower(repository.Name), strings.ToLower(query)) {
			charts = append(charts, ChartInfo{
				Name:        repository.Name + "/example-chart",
				Version:     "1.0.0",
				Description: "Use 'Add Chart Directly' with chart name",
				Repository:  repository.URL,
			})
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"charts": charts})
}

func chartInfoHandler(w http.ResponseWriter, r *http.Request) {
	chart := r.URL.Query().Get("chart")
	version := r.URL.Query().Get("version")

	// Hauler uses Helm Go libraries, not CLI
	// Chart info is obtained when adding via hauler store add chart
	info := fmt.Sprintf("Chart: %s\nVersion: %s\n\nUse 'Add Chart Directly' to add this chart to the store.\nHauler will automatically fetch chart metadata.", chart, version)
	
	respondJSON(w, Response{Success: true, Output: info})
}

func imageSearchHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	registry := r.URL.Query().Get("registry")

	if registry == "" {
		registry = "docker.io"
	}

	images := []ImageInfo{
		{Name: query, Tags: []string{"latest", "stable"}},
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"images": images})
}

func addContentHandler(w http.ResponseWriter, r *http.Request) {
	var req AddContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var args []string
	if req.Type == "chart" {
		args = []string{"store", "add", "chart", req.Name}
		if req.Repository != "" {
			args = append(args, "--repo", req.Repository)
		}
		if req.Version != "" {
			args = append(args, "--version", req.Version)
		}
		if req.Platform != "" && req.Platform != "all" {
			args = append(args, "--platform", req.Platform)
		}
		if req.AddImages {
			args = append(args, "--add-images")
		}
		if req.AddDependencies {
			args = append(args, "--add-dependencies")
		}
		if req.Registry != "" {
			args = append(args, "--registry", req.Registry)
		}
		if req.Rewrite != "" {
			args = append(args, "--rewrite", req.Rewrite)
		}
		if req.Username != "" {
			args = append(args, "--username", req.Username)
		}
		if req.Password != "" {
			args = append(args, "--password", req.Password)
		}
		if req.InsecureSkipTLS {
			args = append(args, "--insecure-skip-tls-verify")
		}
		if req.KubeVersion != "" {
			args = append(args, "--kube-version", req.KubeVersion)
		}
		if req.Verify {
			args = append(args, "--verify")
		}
		if req.Values != "" {
			valuesPath := filepath.Join("/data/config/values", req.Values)
			args = append(args, "--values", valuesPath)
		}
	} else if req.Type == "image" {
		args = []string{"store", "add", "image", req.Name}
		if req.Platform != "" && req.Platform != "all" {
			args = append(args, "--platform", req.Platform)
		}
		if req.Key != "" {
			keyPath := filepath.Join("/data/config/keys", req.Key)
			args = append(args, "--key", keyPath)
		}
		if req.Rewrite != "" {
			args = append(args, "--rewrite", req.Rewrite)
		}
		if req.CertIdentity != "" {
			args = append(args, "--certificate-identity", req.CertIdentity)
		}
		if req.CertIdentityRegexp != "" {
			args = append(args, "--certificate-identity-regexp", req.CertIdentityRegexp)
		}
		if req.CertOIDCIssuer != "" {
			args = append(args, "--certificate-oidc-issuer", req.CertOIDCIssuer)
		}
		if req.CertOIDCIssuerRegexp != "" {
			args = append(args, "--certificate-oidc-issuer-regexp", req.CertOIDCIssuerRegexp)
		}
		if req.CertGithubWorkflow != "" {
			args = append(args, "--certificate-github-workflow-repository", req.CertGithubWorkflow)
		}
		if req.UseTlogVerify {
			args = append(args, "--use-tlog-verify")
		}
	}

	output, err := executeHauler(args[0], args[1:]...)
	respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]bool{"healthy": true})
}

func storeInfoHandler(w http.ResponseWriter, r *http.Request) {
	output, err := executeHauler("store", "info")
	if err != nil {
		respondError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, Response{Success: true, Output: output})
}

func storeSyncHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename                string `json:"filename"`
		Products                string `json:"products"`
		ProductRegistry         string `json:"productRegistry"`
		Platform                string `json:"platform"`
		Key                     string `json:"key"`
		Registry                string `json:"registry"`
		Rewrite                 string `json:"rewrite"`
		CertIdentity            string `json:"certIdentity"`
		CertIdentityRegexp      string `json:"certIdentityRegexp"`
		CertOIDCIssuer          string `json:"certOidcIssuer"`
		CertOIDCIssuerRegexp    string `json:"certOidcIssuerRegexp"`
		CertGithubWorkflow      string `json:"certGithubWorkflow"`
		UseTlogVerify           bool   `json:"useTlogVerify"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	args := []string{"store", "sync"}
	if req.Filename != "" {
		args = append(args, "--filename", filepath.Join("/data/manifests", req.Filename))
	}
	if req.Products != "" {
		args = append(args, "--products", req.Products)
	}
	if req.ProductRegistry != "" {
		args = append(args, "--product-registry", req.ProductRegistry)
	}
	if req.Platform != "" && req.Platform != "all" {
		args = append(args, "--platform", req.Platform)
	}
	if req.Key != "" {
		keyPath := filepath.Join("/data/config/keys", req.Key)
		args = append(args, "--key", keyPath)
	}
	if req.Registry != "" {
		args = append(args, "--registry", req.Registry)
	}
	if req.Rewrite != "" {
		args = append(args, "--rewrite", req.Rewrite)
	}
	if req.CertIdentity != "" {
		args = append(args, "--certificate-identity", req.CertIdentity)
	}
	if req.CertIdentityRegexp != "" {
		args = append(args, "--certificate-identity-regexp", req.CertIdentityRegexp)
	}
	if req.CertOIDCIssuer != "" {
		args = append(args, "--certificate-oidc-issuer", req.CertOIDCIssuer)
	}
	if req.CertOIDCIssuerRegexp != "" {
		args = append(args, "--certificate-oidc-issuer-regexp", req.CertOIDCIssuerRegexp)
	}
	if req.CertGithubWorkflow != "" {
		args = append(args, "--certificate-github-workflow-repository", req.CertGithubWorkflow)
	}
	if req.UseTlogVerify {
		args = append(args, "--use-tlog-verify")
	}

	output, err := executeHauler(args[0], args[1:]...)
	respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func storeSaveHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename    string `json:"filename"`
		Platform    string `json:"platform"`
		Containerd  bool   `json:"containerd"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	filename := "haul.tar.zst"
	if req.Filename != "" {
		filename = req.Filename
	}

	args := []string{"store", "save", "--filename", filepath.Join("/data/hauls", filename)}
	if req.Platform != "" && req.Platform != "all" {
		args = append(args, "--platform", req.Platform)
	}
	if req.Containerd {
		args = append(args, "--containerd")
	}

	output, err := executeHauler(args[0], args[1:]...)
	respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func storeLoadHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename string `json:"filename"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	args := []string{"store", "load"}
	if req.Filename != "" {
		args = append(args, "--filename", filepath.Join("/data/hauls", req.Filename))
	}

	output, err := executeHauler(args[0], args[1:]...)
	respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func fileUploadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(100 << 20)
	file, handler, err := r.FormFile("file")
	if err != nil {
		respondError(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileType := r.FormValue("type")
	baseDir := "/data/manifests"
	if fileType == "haul" {
		baseDir = "/data/hauls"
	}
	destPath, err := safePath(baseDir, handler.Filename)
	if err != nil {
		respondError(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	os.MkdirAll(filepath.Dir(destPath), 0755)
	dst, err := os.Create(destPath)
	if err != nil {
		respondError(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	io.Copy(dst, file)
	respondJSON(w, Response{Success: true, Output: "File uploaded successfully"})
}

func fileListHandler(w http.ResponseWriter, r *http.Request) {
	fileType := r.URL.Query().Get("type")
	var dir string
	if fileType == "haul" {
		dir = "/data/hauls"
	} else {
		dir = "/data/manifests"
	}

	files := []string{}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, e.Name())
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"files": files})
}

func fileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filename := vars["filename"]
	fileType := r.URL.Query().Get("type")

	baseDir := "/data/manifests"
	if fileType == "haul" {
		baseDir = "/data/hauls"
	}
	filePath, err := safePath(baseDir, filename)
	if err != nil {
		respondError(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		respondError(w, "File not found", http.StatusNotFound)
		return
	}

	safeName := filepath.Base(filename)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+safeName+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, filePath)
}

func fileDeleteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filename := vars["filename"]
	fileType := r.URL.Query().Get("type")

	baseDir := "/data/manifests"
	if fileType == "haul" {
		baseDir = "/data/hauls"
	}
	filePath, err := safePath(baseDir, filename)
	if err != nil {
		respondError(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	if err := os.Remove(filePath); err != nil {
		respondError(w, "Failed to delete file", http.StatusInternalServerError)
		return
	}

	respondJSON(w, Response{Success: true, Output: "File deleted successfully"})
}

func storeClearHandler(w http.ResponseWriter, r *http.Request) {
	listOutput, err := executeHauler("store", "info")
	if err != nil {
		respondJSON(w, Response{Success: false, Output: listOutput, Error: err.Error()})
		return
	}
	
	artifacts := parseArtifacts(listOutput)
	if len(artifacts) == 0 {
		respondJSON(w, Response{Success: true, Output: "Store is already empty"})
		return
	}
	
	var output strings.Builder
	output.WriteString(fmt.Sprintf("Removing %d artifacts...\n", len(artifacts)))
	
	for _, artifact := range artifacts {
		output.WriteString(fmt.Sprintf("Removing: %s\n", artifact))
		_, err := executeHauler("store", "remove", artifact, "--force")
		if err != nil {
			output.WriteString(fmt.Sprintf("  Error: %s\n", err.Error()))
		} else {
			output.WriteString("  ✓ Removed\n")
		}
	}
	
	output.WriteString("\nStore cleared successfully")
	respondJSON(w, Response{Success: true, Output: output.String()})
}

func systemResetHandler(w http.ResponseWriter, r *http.Request) {
	var output strings.Builder
	
	listOutput, err := executeHauler("store", "info")
	if err == nil {
		artifacts := parseArtifacts(listOutput)
		if len(artifacts) > 0 {
			output.WriteString(fmt.Sprintf("Removing %d artifacts from store...\n", len(artifacts)))
			for _, artifact := range artifacts {
				executeHauler("store", "remove", artifact, "--force")
			}
			output.WriteString("Store cleared\n")
		} else {
			output.WriteString("Store already empty\n")
		}
	}
	
	os.RemoveAll("/data/manifests")
	os.RemoveAll("/data/hauls")
	os.MkdirAll("/data/manifests", 0755)
	os.MkdirAll("/data/hauls", 0755)
	output.WriteString("Manifests and hauls cleared\n")
	output.WriteString("\nSystem reset complete")
	
	respondJSON(w, Response{Success: true, Output: output.String()})
}

func loadRegistries() {
	regFile := "/data/config/registries.json"
	data, err := os.ReadFile(regFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, &registries)
}

func saveRegistries() error {
	regFile := "/data/config/registries.json"
	os.MkdirAll(filepath.Dir(regFile), 0755)
	data, err := json.Marshal(registries)
	if err != nil {
		return err
	}
	return os.WriteFile(regFile, data, 0600)
}

func registryConfigureHandler(w http.ResponseWriter, r *http.Request) {
	var reg RegistryConfig
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		respondError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	registriesMux.Lock()
	registries[reg.Name] = reg
	registriesMux.Unlock()

	if err := saveRegistries(); err != nil {
		respondError(w, "Failed to save registry", http.StatusInternalServerError)
		return
	}

	respondJSON(w, Response{Success: true, Output: "Registry configured successfully"})
}

func registryListHandler(w http.ResponseWriter, r *http.Request) {
	registriesMux.RLock()
	defer registriesMux.RUnlock()

	regList := make([]RegistryConfig, 0, len(registries))
	for _, reg := range registries {
		safeCopy := reg
		safeCopy.Password = "***"
		regList = append(regList, safeCopy)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"registries": regList})
}

func registryRemoveHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	registriesMux.Lock()
	delete(registries, name)
	registriesMux.Unlock()

	if err := saveRegistries(); err != nil {
		respondError(w, "Failed to save registries", http.StatusInternalServerError)
		return
	}

	respondJSON(w, Response{Success: true, Output: "Registry removed successfully"})
}

func registryTestHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	registriesMux.RLock()
	reg, exists := registries[req.Name]
	registriesMux.RUnlock()

	if !exists {
		respondError(w, "Registry not found", http.StatusNotFound)
		return
	}

	respondJSON(w, Response{Success: true, Output: fmt.Sprintf("Connection test to %s would be performed here", reg.URL)})
}

func registryPushHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RegistryName string   `json:"registryName"`
		Content      []string `json:"content"`
		PlainHTTP    bool     `json:"plainHttp"`
		Only         string   `json:"only"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	registriesMux.RLock()
	reg, exists := registries[req.RegistryName]
	registriesMux.RUnlock()

	if !exists {
		respondError(w, "Registry not found", http.StatusNotFound)
		return
	}

	var output strings.Builder
	
	if reg.Username != "" && reg.Password != "" {
		output.WriteString("Logging in to registry...\n")
		cmd := exec.Command("hauler", "login", reg.URL, "-u", reg.Username, "-p", reg.Password)
		env := append(os.Environ(), "HAULER_STORE=/data/store")
		// Add CA certificate if it exists
		if _, err := os.Stat("/data/config/ca-cert.crt"); err == nil {
			env = append(env, "SSL_CERT_FILE=/data/config/ca-cert.crt")
		}
		cmd.Env = env
		loginOutput, err := cmd.CombinedOutput()
		if err != nil {
			respondJSON(w, Response{Success: false, Output: output.String() + string(loginOutput), Error: "Login failed: " + err.Error()})
			return
		}
		output.WriteString("Login successful\n\n")
	}

	args := []string{"store", "copy"}
	if reg.Insecure {
		args = append(args, "--insecure")
	}
	if req.PlainHTTP {
		args = append(args, "--plain-http")
	}
	if req.Only != "" {
		args = append(args, "--only", req.Only)
	}
	args = append(args, "registry://"+reg.URL)

	output.WriteString("Pushing to registry...\n")
	copyOutput, err := executeHauler(args[0], args[1:]...)
	output.WriteString(copyOutput)
	
	respondJSON(w, Response{Success: err == nil, Output: output.String(), Error: errString(err)})
}

func storeAddFileHandler(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	
	if strings.Contains(contentType, "application/json") {
		var req struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "Invalid request", http.StatusBadRequest)
			return
		}
		
		args := []string{"store", "add", "file", req.URL}
		if req.Name != "" {
			args = append(args, "--name", req.Name)
		}
		
		output, err := executeHauler(args[0], args[1:]...)
		respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
		return
	}
	
	r.ParseMultipartForm(100 << 20)
	file, handler, err := r.FormFile("file")
	if err != nil {
		respondError(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	customName := r.FormValue("name")
	tempPath, err := safePath("/tmp", handler.Filename)
	if err != nil {
		respondError(w, "Invalid filename", http.StatusBadRequest)
		return
	}
	dst, err := os.Create(tempPath)
	if err != nil {
		respondError(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	io.Copy(dst, file)
	dst.Close()

	args := []string{"store", "add", "file", tempPath}
	if customName != "" {
		args = append(args, "--name", customName)
	}
	
	output, err := executeHauler(args[0], args[1:]...)
	os.Remove(tempPath)
	
	respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func storeExtractHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OutputDir string `json:"outputDir"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	outputDir := "/data/extracted"
	if req.OutputDir != "" {
		outputDir = filepath.Join("/data", req.OutputDir)
	}
	os.MkdirAll(outputDir, 0755)

	output, err := executeHauler("store", "extract", "-o", outputDir)
	respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func storeArtifactsHandler(w http.ResponseWriter, r *http.Request) {
	output, err := executeHauler("store", "info")
	if err != nil {
		respondError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	artifacts := parseArtifacts(output)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"artifacts": artifacts,
		"count":     len(artifacts),
		"raw":       output,
	})
}

func parseArtifacts(output string) []string {
	artifacts := []string{}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		reference := strings.TrimSpace(parts[1])
		if reference == "" || reference == "REFERENCE" || strings.Contains(reference, "TOTAL") {
			continue
		}
		artifacts = append(artifacts, reference)
	}
	return artifacts
}

func storeRemoveHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	artifact := vars["artifact"]
	force := r.URL.Query().Get("force") == "true"

	args := []string{"store", "remove", artifact}
	if force {
		args = append(args, "--force")
	}

	output, err := executeHauler(args[0], args[1:]...)
	respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func registryLoginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Registry string `json:"registry"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	cmd := exec.Command("hauler", "login", req.Registry, "-u", req.Username, "-p", req.Password)
	env := append(os.Environ(), "HAULER_STORE=/data/store")
	// Add CA certificate if it exists
	if _, err := os.Stat("/data/config/ca-cert.crt"); err == nil {
		env = append(env, "SSL_CERT_FILE=/data/config/ca-cert.crt")
	}
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	
	respondJSON(w, Response{Success: err == nil, Output: string(output), Error: errString(err)})
}

func registryLogoutHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Registry string `json:"registry"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	output, err := executeHauler("logout", req.Registry)
	respondJSON(w, Response{Success: err == nil, Output: output, Error: errString(err)})
}

func keyUploadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20)
	file, handler, err := r.FormFile("key")
	if err != nil {
		respondError(w, "Failed to read key file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	keyPath, err := safePath("/data/config/keys", handler.Filename)
	if err != nil {
		respondError(w, "Invalid filename", http.StatusBadRequest)
		return
	}
	os.MkdirAll(filepath.Dir(keyPath), 0755)
	dst, err := os.Create(keyPath)
	if err != nil {
		respondError(w, "Failed to save key", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	io.Copy(dst, file)
	respondJSON(w, Response{Success: true, Output: "Key uploaded: " + filepath.Base(handler.Filename)})
}

func keyListHandler(w http.ResponseWriter, r *http.Request) {
	keyDir := "/data/config/keys"
	keys := []string{}
	if entries, err := os.ReadDir(keyDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				keys = append(keys, e.Name())
			}
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"keys": keys})
}

func tlsCertUploadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20)
	file, handler, err := r.FormFile("cert")
	if err != nil {
		respondError(w, "Failed to read cert file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	certPath, err := safePath("/data/config/certs", handler.Filename)
	if err != nil {
		respondError(w, "Invalid filename", http.StatusBadRequest)
		return
	}
	os.MkdirAll(filepath.Dir(certPath), 0755)
	dst, err := os.Create(certPath)
	if err != nil {
		respondError(w, "Failed to save cert", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	io.Copy(dst, file)
	respondJSON(w, Response{Success: true, Output: "TLS cert uploaded: " + filepath.Base(handler.Filename)})
}

func tlsCertListHandler(w http.ResponseWriter, r *http.Request) {
	certDir := "/data/config/certs"
	certs := []string{}
	if entries, err := os.ReadDir(certDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				certs = append(certs, e.Name())
			}
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"certs": certs})
}

func valuesUploadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20)
	file, handler, err := r.FormFile("values")
	if err != nil {
		respondError(w, "Failed to read values file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	valuesPath, err := safePath("/data/config/values", handler.Filename)
	if err != nil {
		respondError(w, "Invalid filename", http.StatusBadRequest)
		return
	}
	os.MkdirAll(filepath.Dir(valuesPath), 0755)
	dst, err := os.Create(valuesPath)
	if err != nil {
		respondError(w, "Failed to save values file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	io.Copy(dst, file)
	respondJSON(w, Response{Success: true, Output: "Values file uploaded: " + filepath.Base(handler.Filename)})
}

func valuesListHandler(w http.ResponseWriter, r *http.Request) {
	valuesDir := "/data/config/values"
	values := []string{}
	if entries, err := os.ReadDir(valuesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				values = append(values, e.Name())
			}
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"values": values})
}

func certUploadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20)
	file, _, err := r.FormFile("cert")
	if err != nil {
		respondError(w, "Failed to read certificate", http.StatusBadRequest)
		return
	}
	defer file.Close()

	certData, err := io.ReadAll(file)
	if err != nil {
		respondError(w, "Failed to read certificate data", http.StatusBadRequest)
		return
	}

	// Validate that the uploaded data contains at least one valid PEM certificate
	block, _ := pem.Decode(certData)
	if block == nil {
		respondError(w, "Invalid certificate: not a valid PEM file", http.StatusBadRequest)
		return
	}
	if block.Type != "CERTIFICATE" {
		respondError(w, "Invalid certificate: PEM block is not a CERTIFICATE", http.StatusBadRequest)
		return
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		respondError(w, "Invalid certificate: "+err.Error(), http.StatusBadRequest)
		return
	}

	certPath := "/data/config/ca-cert.crt"
	os.MkdirAll(filepath.Dir(certPath), 0755)
	if err := os.WriteFile(certPath, certData, 0644); err != nil {
		respondError(w, "Failed to save certificate", http.StatusInternalServerError)
		return
	}

	exec.Command("update-ca-certificates").Run()

	respondJSON(w, Response{Success: true, Output: "Certificate uploaded and installed"})
}

var serveCmd *exec.Cmd
var serveMux sync.Mutex

func serveStartHandler(w http.ResponseWriter, r *http.Request) {
	serveMux.Lock()
	defer serveMux.Unlock()

	if serveCmd != nil && serveCmd.Process != nil {
		respondError(w, "Server already running", http.StatusBadRequest)
		return
	}

	var req struct {
		Port       string `json:"port"`
		Mode       string `json:"mode"`
		Readonly   bool   `json:"readonly"`
		TLSCert    string `json:"tlsCert"`
		TLSKey     string `json:"tlsKey"`
		Timeout    int    `json:"timeout"`
		Config     string `json:"config"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Port == "" {
		if req.Mode == "fileserver" {
			req.Port = "8081"
		} else {
			req.Port = "5000"
		}
	}
	
	mode := "registry"
	if req.Mode == "fileserver" {
		mode = "fileserver"
	}

	args := []string{"store", "serve", mode, "--port", req.Port}
	
	if mode == "registry" && !req.Readonly {
		args = append(args, "--readonly=false")
	}
	
	if req.TLSCert != "" && req.TLSKey != "" {
		certPath := filepath.Join("/data/config/certs", req.TLSCert)
		keyPath := filepath.Join("/data/config/certs", req.TLSKey)
		args = append(args, "--tls-cert", certPath, "--tls-key", keyPath)
	}
	
	if mode == "fileserver" && req.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", req.Timeout))
	}
	
	if req.Config != "" {
		configPath := filepath.Join("/data/config", req.Config)
		args = append(args, "--config", configPath)
	}

	serveCmd = exec.Command("hauler", args...)
	serveCmd.Env = append(os.Environ(), "HAULER_STORE=/data/store")
	
	go func() {
		serveCmd.Run()
	}()

	time.Sleep(500 * time.Millisecond)
	respondJSON(w, Response{Success: true, Output: fmt.Sprintf("%s started on port %s", mode, req.Port)})
}

func serveStopHandler(w http.ResponseWriter, r *http.Request) {
	serveMux.Lock()
	defer serveMux.Unlock()

	if serveCmd != nil && serveCmd.Process != nil {
		serveCmd.Process.Kill()
		serveCmd = nil
		respondJSON(w, Response{Success: true, Output: "Server stopped"})
	} else {
		respondError(w, "No server running", http.StatusBadRequest)
	}
}

func serveStatusHandler(w http.ResponseWriter, r *http.Request) {
	serveMux.Lock()
	defer serveMux.Unlock()

	running := serveCmd != nil && serveCmd.Process != nil
	json.NewEncoder(w).Encode(map[string]bool{"running": running})
}

func logsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastSent := 0
	for range ticker.C {
		logMux.Lock()
		if len(logLines) > lastSent {
			for i := lastSent; i < len(logLines); i++ {
				conn.WriteMessage(websocket.TextMessage, []byte(logLines[i]))
			}
			lastSent = len(logLines)
		}
		logMux.Unlock()
	}
}

func executeHauler(command string, args ...string) (string, error) {
	fullArgs := append([]string{command}, args...)
	cmd := exec.Command("hauler", fullArgs...)
	env := append(os.Environ(), "HAULER_STORE=/data/store")
	
	// Add CA certificate if it exists
	if _, err := os.Stat("/data/config/ca-cert.crt"); err == nil {
		env = append(env, "SSL_CERT_FILE=/data/config/ca-cert.crt")
	}
	cmd.Env = env
	
	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	
	logMux.Lock()
	logLines = append(logLines, fmt.Sprintf("[%s] hauler %s: %s", time.Now().Format("15:04:05"), strings.Join(fullArgs, " "), outputStr))
	if len(logLines) > 1000 {
		logLines = logLines[len(logLines)-1000:]
	}
	logMux.Unlock()
	
	return outputStr, err
}

func respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(Response{Success: false, Error: message})
}

func errString(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
