let ws;
let manifestContent = [];

function showTab(tabName) {
    document.querySelectorAll('.tab-content').forEach(el => el.classList.add('hidden'));
    document.getElementById(tabName).classList.remove('hidden');
    document.querySelectorAll('.nav-btn').forEach(el => el.classList.remove('bg-gray-700'));
    event.target.classList.add('bg-gray-700');
    
    if (tabName === 'manifests') loadFileList('manifest');
    if (tabName === 'hauls') loadFileList('haul');
    if (tabName === 'logs') connectWebSocket();
    if (tabName === 'repositories') loadRepositories();
    if (tabName === 'manifest-builder') updateManifestPreview();
}

async function apiCall(endpoint, method = 'GET', body = null) {
    const options = { method, headers: { 'Content-Type': 'application/json' } };
    if (body) options.body = JSON.stringify(body);
    const res = await fetch(`/api/${endpoint}`, options);
    return res.json();
}

let selectedCharts = {};
let repoChartsData = {};

async function browseRepoCharts(repoName) {
    const data = await apiCall(`repos/charts/${repoName}`);
    
    // Check if this is an OCI registry
    if (data.isOCI) {
        alert(`OCI Registry Detected\n\n${data.message}\n\nOCI registries (oci://) don't support browsing. You'll need to:\n1. Go to the "Add Charts" tab\n2. Manually enter the chart name\n3. Specify the OCI repository URL`);
        return;
    }
    
    if (!data.charts || Object.keys(data.charts).length === 0) {
        alert('No charts found in this repository');
        return;
    }
    
    repoChartsData = data;
    selectedCharts = {};
    
    const modal = document.getElementById('chartBrowserModal');
    const listEl = document.getElementById('chartBrowserList');
    
    listEl.innerHTML = Object.keys(data.charts).sort().map(chartName => {
        const versions = data.charts[chartName];
        const details = data.details[chartName];
        return `
            <div class="bg-gray-900 p-4 rounded border border-gray-700">
                <div class="flex items-start gap-3">
                    <input type="checkbox" id="chart_${chartName}" onchange="toggleChart('${chartName}')" 
                           class="mt-1 w-4 h-4">
                    <div class="flex-1">
                        <label for="chart_${chartName}" class="font-bold cursor-pointer">${chartName}</label>
                        <p class="text-gray-400 text-sm">${details.description || 'No description'}</p>
                        <select id="version_${chartName}" class="mt-2 bg-gray-800 border border-gray-600 rounded p-1 text-sm" disabled>
                            ${versions.map(v => `<option value="${v}">${v}</option>`).join('')}
                        </select>
                    </div>
                </div>
            </div>
        `;
    }).join('');
    
    modal.classList.remove('hidden');
    updateChartPreview();
}

function toggleChart(chartName) {
    const checkbox = document.getElementById(`chart_${chartName}`);
    const versionSelect = document.getElementById(`version_${chartName}`);
    
    if (checkbox.checked) {
        selectedCharts[chartName] = versionSelect.value;
        versionSelect.disabled = false;
    } else {
        delete selectedCharts[chartName];
        versionSelect.disabled = true;
    }
    
    updateChartPreview();
}

function updateChartPreview() {
    const previewEl = document.getElementById('chartPreview');
    const count = Object.keys(selectedCharts).length;
    
    if (count === 0) {
        previewEl.innerHTML = '<p class="text-gray-400">No charts selected</p>';
        return;
    }
    
    previewEl.innerHTML = `
        <p class="font-bold mb-2">${count} chart(s) selected:</p>
        ${Object.entries(selectedCharts).map(([name, version]) => 
            `<div class="text-sm">📦 ${name}:${version}</div>`
        ).join('')}
    `;
}

function closeChartBrowser() {
    document.getElementById('chartBrowserModal').classList.add('hidden');
}

function showImageSelectionModal() {
    const charts = Object.entries(selectedCharts);
    if (charts.length === 0) return alert('No charts selected');
    
    document.getElementById('imageSelectionModal').classList.remove('hidden');
}

function closeImageSelectionModal() {
    document.getElementById('imageSelectionModal').classList.add('hidden');
}

async function processCharts(includeImages) {
    closeImageSelectionModal();
    
    const rewriteBase = document.getElementById('batchChartRewrite')?.value || '';
    const rewriteExact = document.getElementById('batchChartRewriteExact')?.checked || false;
    const outputEl = document.getElementById('chartBatchOutput');
    
    const charts = Object.entries(selectedCharts);
    outputEl.textContent = `Adding ${charts.length} chart(s)${includeImages ? ' with images' : ''}...\n`;
    
    for (const [name, version] of charts) {
        const details = repoChartsData.details[name];
        outputEl.textContent += `\nAdding ${name}:${version}...\n`;
        
        const rewrite = rewriteBase ? (rewriteExact ? rewriteBase : `${rewriteBase}/${name}:${version}`) : '';
        
        const data = await apiCall('store/add-content', 'POST', {
            type: 'chart',
            name: name,
            version: version,
            repository: details.repository,
            rewrite: rewrite,
            addImages: includeImages,
            addDependencies: includeImages
        });
        
        outputEl.textContent += data.output + '\n';
    }
    
    outputEl.textContent += '\n✅ Batch add complete!';
    setTimeout(refreshStoreInfo, 1000);
}
async function addChartDirectFromForm() {
    const name = document.getElementById('chartName').value;
    const repo = document.getElementById('chartRepo').value;
    const version = document.getElementById('chartVersion').value;
    const platform = document.getElementById('chartPlatform').value;
    const rewriteBase = document.getElementById('chartRewrite')?.value || '';
    const username = document.getElementById('chartUsername')?.value || '';
    const password = document.getElementById('chartPassword')?.value || '';
    const insecureSkipTls = document.getElementById('chartInsecureSkipTLS')?.checked || false;
    const kubeVersion = document.getElementById('chartKubeVersion')?.value || '';
    const verify = document.getElementById('chartVerify')?.checked || false;
    const values = document.getElementById('chartValues')?.value || '';
    
    if (!name || !repo) return alert('Chart name and repository are required');
    
    const skipImages = !confirm('Extract and add images from chart?\n\nOK = Add chart + images\nCancel = Chart only');
    
    const rewrite = rewriteBase && version ? `${rewriteBase}/${name}:${version}` : (rewriteBase ? `${rewriteBase}/${name}` : '');
    
    const outputEl = document.getElementById('chartOutput');
    outputEl.textContent = 'Adding chart...';
    
    const data = await apiCall('store/add-content', 'POST', {
        type: 'chart',
        name, version: version || '', repository: repo, platform, rewrite,
        addImages: !skipImages, addDependencies: !skipImages,
        username, password, insecureSkipTls, kubeVersion, verify, values
    });
    
    outputEl.textContent = data.output || data.error;
    if (data.success || (data.output && data.output.includes('successfully added chart'))) {
        setTimeout(refreshStoreInfo, 1000);
    }
}

// Image Addition
async function addImageDirectFromForm() {
    const name = document.getElementById('imageName').value;
    const platform = document.getElementById('imagePlatform').value;
    const key = document.getElementById('imageKey').value;
    const rewrite = document.getElementById('imageRewrite')?.value || '';
    const certIdentity = document.getElementById('imageCertIdentity')?.value || '';
    const certIdentityRegexp = document.getElementById('imageCertIdentityRegexp')?.value || '';
    const certOidcIssuer = document.getElementById('imageCertOIDCIssuer')?.value || '';
    const certOidcIssuerRegexp = document.getElementById('imageCertOIDCIssuerRegexp')?.value || '';
    const certGithubWorkflow = document.getElementById('imageCertGithubWorkflow')?.value || '';
    const useTlogVerify = document.getElementById('imageUseTlogVerify')?.checked || false;
    
    if (!name) return alert('Image name is required');
    
    const outputEl = document.getElementById('imageOutput');
    outputEl.textContent = 'Adding image...';
    
    const data = await apiCall('store/add-content', 'POST', {
        type: 'image', name, platform, key, rewrite,
        certIdentity, certIdentityRegexp, certOidcIssuer, certOidcIssuerRegexp,
        certGithubWorkflow, useTlogVerify
    });
    
    outputEl.textContent = data.output || data.error;
    if (data.success) setTimeout(refreshStoreInfo, 1000);
}
async function addRepository() {
    const name = document.getElementById('repoName').value;
    const url = document.getElementById('repoURL').value;
    
    if (!name || !url) return alert('Please provide name and URL');
    
    const data = await apiCall('repos/add', 'POST', { name, url });
    alert(data.output || data.error);
    loadRepositories();
    document.getElementById('repoName').value = '';
    document.getElementById('repoURL').value = '';
}

async function loadRepositories() {
    const data = await apiCall('repos/list');
    const listEl = document.getElementById('repoList');
    const countEl = document.getElementById('repoCount');
    
    if (!data.repositories || data.repositories.length === 0) {
        listEl.innerHTML = '<p class="text-gray-400">No repositories configured</p>';
        if (countEl) countEl.textContent = '0';
        return;
    }
    
    if (countEl) countEl.textContent = data.repositories.length;
    
    listEl.innerHTML = data.repositories.map(repo => `
        <div class="flex justify-between items-center bg-gray-900 p-3 rounded">
            <div>
                <span class="font-bold">${repo.name}</span>
                <span class="text-gray-400 text-sm ml-2">${repo.url}</span>
            </div>
            <div class="flex gap-2">
                <button onclick="browseRepoCharts('${repo.name.replace(/'/g, "\\'")}')"
                        class="text-blue-400 hover:text-blue-300">
                    <i class="fas fa-list"></i> Browse
                </button>
                <button onclick="removeRepository('${repo.name.replace(/'/g, "\\'")}')"
                        class="text-red-400 hover:text-red-300">
                    <i class="fas fa-trash"></i>
                </button>
            </div>
        </div>
    `).join('');
}

async function removeRepository(name) {
    if (!confirm(`Remove repository ${name}?`)) return;
    await fetch(`/api/repos/remove/${name}`, { method: 'DELETE' });
    loadRepositories();
}

// Repository Management

// Manifest Builder
function updateManifestPreview() {
    const itemsEl = document.getElementById('manifestItems');
    const previewEl = document.getElementById('manifestPreview');
    
    if (manifestContent.length === 0) {
        itemsEl.innerHTML = '<p class="text-gray-400">No content selected</p>';
        previewEl.textContent = '# No content selected';
        return;
    }
    
    itemsEl.innerHTML = manifestContent.map((item, idx) => `
        <div class="bg-gray-900 p-3 rounded border border-gray-700 flex justify-between items-center">
            <div>
                <span class="font-bold">${item.type === 'chart' ? '📦' : '🐳'} ${item.name}</span>
                ${item.version ? `<span class="text-gray-400 text-sm ml-2">v${item.version}</span>` : ''}
            </div>
            <button onclick="removeFromManifest(${idx})" class="text-red-400 hover:text-red-300">
                <i class="fas fa-times"></i>
            </button>
        </div>
    `).join('');
    
    const yaml = generateYAML();
    previewEl.textContent = yaml;
}

function generateYAML() {
    let yaml = 'apiVersion: v1\n';
    
    const images = manifestContent.filter(i => i.type === 'image');
    if (images.length > 0) {
        yaml += 'kind: Images\n';
        yaml += 'spec:\n';
        yaml += '  images:\n';
        images.forEach(img => {
            yaml += `    - name: ${img.name}\n`;
        });
        yaml += '---\n';
    }
    
    const charts = manifestContent.filter(i => i.type === 'chart');
    if (charts.length > 0) {
        yaml += 'apiVersion: v1\n';
        yaml += 'kind: Charts\n';
        yaml += 'spec:\n';
        yaml += '  charts:\n';
        charts.forEach(chart => {
            yaml += `    - name: ${chart.name}\n`;
            if (chart.repository) yaml += `      repoURL: ${chart.repository}\n`;
            if (chart.version) yaml += `      version: ${chart.version}\n`;
            if (chart.addImages) yaml += `      addImages: true\n`;
            if (chart.addDependencies) yaml += `      addDependencies: true\n`;
        });
    }
    
    return yaml;
}

function removeFromManifest(idx) {
    manifestContent.splice(idx, 1);
    updateManifestPreview();
}

function clearManifest() {
    if (!confirm('Clear all content?')) return;
    manifestContent = [];
    updateManifestPreview();
}

async function saveManifestFile() {
    if (manifestContent.length === 0) return alert('No content to save');
    
    const yaml = generateYAML();
    const filename = prompt('Manifest filename:', 'my-manifest.yaml');
    if (!filename) return;
    
    const blob = new Blob([yaml], { type: 'text/yaml' });
    const formData = new FormData();
    formData.append('file', blob, filename);
    formData.append('type', 'manifest');
    
    const res = await fetch('/api/files/upload', { method: 'POST', body: formData });
    const data = await res.json();
    alert(data.output || data.error);
}

// Existing functions
async function refreshStoreInfo() {
    const data = await apiCall('store/info');
    document.getElementById('storeInfo').textContent = data.output || data.error;
    const previewEl = document.getElementById('storePreview');
    if (previewEl) {
        previewEl.textContent = data.output || data.error;
    }
}

async function syncStore() {
    const filename = document.getElementById('syncManifest').value;
    const products = document.getElementById('syncProducts').value;
    const productRegistry = document.getElementById('syncProductRegistry').value;
    const platform = document.getElementById('syncPlatform').value;
    const key = document.getElementById('syncKey').value;
    const registry = document.getElementById('syncRegistry')?.value || '';
    const rewrite = document.getElementById('syncRewrite')?.value || '';
    const certIdentity = document.getElementById('syncCertIdentity')?.value || '';
    const certIdentityRegexp = document.getElementById('syncCertIdentityRegexp')?.value || '';
    const certOidcIssuer = document.getElementById('syncCertOIDCIssuer')?.value || '';
    const certOidcIssuerRegexp = document.getElementById('syncCertOIDCIssuerRegexp')?.value || '';
    const certGithubWorkflow = document.getElementById('syncCertGithubWorkflow')?.value || '';
    const useTlogVerify = document.getElementById('syncUseTlogVerify')?.checked || false;
    
    const data = await apiCall('store/sync', 'POST', {
        filename, products, productRegistry, platform, key, registry, rewrite,
        certIdentity, certIdentityRegexp, certOidcIssuer, certOidcIssuerRegexp,
        certGithubWorkflow, useTlogVerify
    });
    document.getElementById('storeOutput').textContent = data.output || data.error;
}

async function saveStore() {
    const filename = document.getElementById('saveFilename').value || 'haul.tar.zst';
    const platform = document.getElementById('savePlatform')?.value || 'all';
    const containerd = document.getElementById('saveContainerd')?.checked || false;
    const outputEl = document.getElementById('storeOutput');
    
    outputEl.textContent = 'Creating haul...';
    
    const data = await apiCall('store/save', 'POST', { filename, platform, containerd });
    outputEl.textContent = data.output || data.error;
    
    if (data.success) {
        outputEl.textContent += '\n\n⬇️ Downloading haul...';
        window.location.href = `/api/files/download/${filename}?type=haul`;
    }
}

async function loadStore() {
    const filename = document.getElementById('loadHaul').value;
    const data = await apiCall('store/load', 'POST', { filename });
    document.getElementById('storeOutput').textContent = data.output || data.error;
}

async function uploadFile(type) {
    const fileInput = type === 'haul' ? document.getElementById('haulFile') : document.getElementById('manifestFile');
    const file = fileInput.files[0];
    if (!file) return alert('Select a file');
    
    const formData = new FormData();
    formData.append('file', file);
    formData.append('type', type);
    
    const res = await fetch('/api/files/upload', { method: 'POST', body: formData });
    const data = await res.json();
    alert(data.output || data.error);
    loadFileList(type);
}

async function loadFileList(type) {
    const data = await apiCall(`files/list?type=${type}`);
    const listEl = type === 'haul' ? document.getElementById('haulList') : document.getElementById('manifestList');
    const selectEl = type === 'haul' ? document.getElementById('loadHaul') : document.getElementById('syncManifest');
    
    listEl.innerHTML = data.files.map(f => `
        <div class="flex justify-between items-center bg-gray-900 p-3 rounded">
            <span>${f}</span>
            <div class="flex gap-2">
                <a href="/api/files/download/${f}?type=${type}" class="text-blue-400 hover:text-blue-300">
                    <i class="fas fa-download"></i>
                </a>
                <button onclick="deleteFile('${f}', '${type}')" class="text-red-400 hover:text-red-300">
                    <i class="fas fa-trash"></i>
                </button>
            </div>
        </div>
    `).join('');
    
    selectEl.innerHTML = '<option value="">Select...</option>' + 
        data.files.map(f => `<option value="${f}">${f}</option>`).join('');
}

async function deleteFile(filename, type) {
    if (!confirm(`⚠️ WARNING: Delete ${filename}?\n\nThis action cannot be undone.`)) return;
    
    const res = await fetch(`/api/files/delete/${encodeURIComponent(filename)}?type=${type}`, { method: 'DELETE' });
    const data = await res.json();
    
    if (data.success) {
        alert(`✅ ${filename} deleted successfully`);
        loadFileList(type);
    } else {
        alert(`❌ Failed to delete: ${data.error}`);
    }
}

async function clearStore() {
    if (!confirm('⚠️ WARNING: Clear entire store?\n\nThis will remove ALL content from the store.\nThis action cannot be undone.')) return;
    
    if (!confirm('⚠️ FINAL CONFIRMATION\n\nAre you absolutely sure you want to clear the store?')) return;
    
    const outputEl = document.getElementById('storeOutput');
    outputEl.textContent = 'Clearing store...';
    
    const data = await apiCall('store/clear', 'POST');
    outputEl.textContent = data.output || data.error;
    
    if (data.success) {
        setTimeout(refreshStoreInfo, 1000);
    }
}

async function resetSystem() {
    if (!confirm('⚠️ WARNING: Reset Hauler System?\n\nThis will clear the entire store.\nUploaded files will be preserved.\n\nThis action cannot be undone.')) return;
    
    if (!confirm('⚠️ FINAL CONFIRMATION\n\nAre you absolutely sure?')) return;
    
    const outputEl = document.getElementById('settingsOutput');
    outputEl.textContent = 'Resetting system...';
    
    const data = await apiCall('system/reset', 'POST');
    outputEl.textContent = data.output || data.error;
    
    if (data.success) {
        setTimeout(refreshStoreInfo, 1000);
    }
}

async function configureRegistry() {
    const name = document.getElementById('registryName').value;
    const url = document.getElementById('registryURL').value;
    const username = document.getElementById('registryUsername').value;
    const password = document.getElementById('registryPassword').value;
    const insecure = document.getElementById('registryInsecure').checked;
    
    if (!name || !url) return alert('Name and URL are required');
    
    const data = await apiCall('registry/configure', 'POST', {
        name, url, username, password, insecure
    });
    
    alert(data.output || data.error);
    loadRegistries();
    
    document.getElementById('registryName').value = '';
    document.getElementById('registryURL').value = '';
    document.getElementById('registryUsername').value = '';
    document.getElementById('registryPassword').value = '';
    document.getElementById('registryInsecure').checked = false;
}

async function loadRegistries() {
    const data = await apiCall('registry/list');
    const listEl = document.getElementById('registryList');
    const selectEl = document.getElementById('pushRegistry');
    
    if (!data.registries || data.registries.length === 0) {
        listEl.innerHTML = '<p class="text-gray-400">No registries configured</p>';
        if (selectEl) selectEl.innerHTML = '<option value="">No registries available</option>';
        return;
    }
    
    listEl.innerHTML = data.registries.map(reg => `
        <div class="flex justify-between items-center bg-gray-900 p-3 rounded">
            <div>
                <span class="font-bold">${reg.name}</span>
                <span class="text-gray-400 text-sm ml-2">${reg.url}</span>
            </div>
            <div class="flex gap-2">
                <button onclick="testRegistry('${reg.name}')" class="text-blue-400 hover:text-blue-300">
                    <i class="fas fa-plug"></i> Test
                </button>
                <button onclick="removeRegistry('${reg.name}')" class="text-red-400 hover:text-red-300">
                    <i class="fas fa-trash"></i>
                </button>
            </div>
        </div>
    `).join('');
    
    if (selectEl) {
        selectEl.innerHTML = '<option value="">Select registry...</option>' +
            data.registries.map(r => `<option value="${r.name}">${r.name}</option>`).join('');
    }
}

async function removeRegistry(name) {
    if (!confirm(`Remove registry ${name}?`)) return;
    await fetch(`/api/registry/remove/${name}`, { method: 'DELETE' });
    loadRegistries();
}

async function testRegistry(name) {
    const outputEl = document.getElementById('pushOutput');
    outputEl.textContent = `Testing connection to ${name}...`;
    
    const data = await apiCall('registry/test', 'POST', { name });
    outputEl.textContent = data.success ? 
        `✅ Connection successful to ${name}` : 
        `❌ Connection failed: ${data.error}`;
}

async function pushToRegistry() {
    const registryName = document.getElementById('pushRegistry').value;
    const plainHttp = document.getElementById('pushPlainHTTP')?.checked || false;
    const only = document.getElementById('pushOnly')?.value || '';
    
    if (!registryName) return alert('Select a registry');
    
    if (!confirm(`Push all store content to ${registryName}?\n\nThis may take several minutes.`)) return;
    
    const outputEl = document.getElementById('pushOutput');
    outputEl.textContent = `Pushing to ${registryName}...`;
    
    const data = await apiCall('registry/push', 'POST', { registryName, content: [], plainHttp, only });
    outputEl.textContent = data.output || data.error;
}

async function addFileToStore() {
    const mode = document.querySelector('input[name="fileMode"]:checked').value;
    const outputEl = document.getElementById('fileOutput');
    
    if (mode === 'url') {
        const url = document.getElementById('fileURL').value;
        const name = document.getElementById('fileName').value;
        
        if (!url) return alert('URL required');
        
        outputEl.textContent = 'Adding file from URL...';
        const data = await apiCall('store/add-file', 'POST', {url, name});
        outputEl.textContent = data.output || data.error;
    } else {
        const file = document.getElementById('fileToAdd').files[0];
        const name = document.getElementById('fileNameUpload').value;
        
        if (!file) return alert('Select a file');
        
        const formData = new FormData();
        formData.append('file', file);
        if (name) formData.append('name', name);
        
        outputEl.textContent = 'Uploading file...';
        const res = await fetch('/api/store/add-file', {method: 'POST', body: formData});
        const data = await res.json();
        outputEl.textContent = data.output || data.error;
    }
    
    if (data.success) setTimeout(refreshStoreInfo, 1000);
}

async function extractStore() {
    const outputDir = document.getElementById('extractDir').value || 'extracted';
    
    if (!confirm(`Extract store contents to /data/${outputDir}?`)) return;
    
    const outputEl = document.getElementById('extractOutput');
    outputEl.textContent = 'Extracting...';
    
    const data = await apiCall('store/extract', 'POST', {outputDir});
    outputEl.textContent = data.output || data.error;
}

async function listArtifacts() {
    const data = await apiCall('store/artifacts');
    const listEl = document.getElementById('artifactList');
    
    if (!data.artifacts || data.artifacts.length === 0) {
        listEl.innerHTML = '<p class="text-gray-400">No artifacts in store</p>';
        return;
    }
    
    listEl.innerHTML = data.artifacts.map(artifact => `
        <div class="flex justify-between items-center bg-gray-900 p-3 rounded mb-2">
            <span class="font-mono text-sm">${artifact}</span>
            <button onclick="removeArtifact('${artifact.replace(/'/g, "\\'")}')"
                    class="text-red-400 hover:text-red-300">
                <i class="fas fa-trash"></i> Remove
            </button>
        </div>
    `).join('');
}

async function removeArtifact(artifact) {
    if (!confirm(`⚠️ Remove ${artifact}?\n\nThis cannot be undone.`)) return;
    
    const res = await fetch(`/api/store/remove/${encodeURIComponent(artifact)}?force=true`, {method: 'DELETE'});
    const data = await res.json();
    
    alert(data.success ? '✅ Removed' : `❌ ${data.error}`);
    if (data.success) {
        listArtifacts();
        refreshStoreInfo();
    }
}

async function registryLogin() {
    const registry = document.getElementById('loginRegistry').value;
    const username = document.getElementById('loginUsername').value;
    const password = document.getElementById('loginPassword').value;
    
    if (!registry || !username || !password) return alert('All fields required');
    
    const data = await apiCall('registry/login', 'POST', {registry, username, password});
    document.getElementById('authOutput').textContent = data.output || data.error;
    alert(data.success ? '✅ Logged in' : `❌ ${data.error}`);
    
    document.getElementById('loginPassword').value = '';
}

async function registryLogout() {
    const registry = document.getElementById('logoutRegistry').value;
    if (!registry) return alert('Registry required');
    
    const data = await apiCall('registry/logout', 'POST', {registry});
    document.getElementById('authOutput').textContent = data.output || data.error;
    alert(data.success ? '✅ Logged out' : `❌ ${data.error}`);
}

async function uploadKey() {
    const file = document.getElementById('keyFile').files[0];
    if (!file) return alert('Select a key file');
    
    const formData = new FormData();
    formData.append('key', file);
    
    const res = await fetch('/api/key/upload', {method: 'POST', body: formData});
    const data = await res.json();
    alert(data.output || data.error);
    loadKeys();
}

async function loadKeys() {
    const data = await apiCall('key/list');
    const selects = ['imageKey', 'syncKey'];
    
    selects.forEach(id => {
        const el = document.getElementById(id);
        if (el) {
            el.innerHTML = '<option value="">None</option>' +
                (data.keys || []).map(k => `<option value="${k}">${k}</option>`).join('');
        }
    });
}

async function uploadTLSCert() {
    const file = document.getElementById('tlsCertFile').files[0];
    if (!file) return alert('Select a TLS certificate or key file');
    
    const formData = new FormData();
    formData.append('cert', file);
    
    const res = await fetch('/api/tlscert/upload', {method: 'POST', body: formData});
    const data = await res.json();
    alert(data.output || data.error);
    loadTLSCerts();
}

async function loadTLSCerts() {
    const data = await apiCall('tlscert/list');
    const selects = ['serveTLSCert', 'serveTLSKey'];
    
    selects.forEach(id => {
        const el = document.getElementById(id);
        if (el) {
            el.innerHTML = '<option value="">None</option>' +
                (data.certs || []).map(c => `<option value="${c}">${c}</option>`).join('');
        }
    });
}

async function uploadValues() {
    const file = document.getElementById('valuesFile').files[0];
    if (!file) return alert('Select a Helm values file');
    
    const formData = new FormData();
    formData.append('values', file);
    
    const res = await fetch('/api/values/upload', {method: 'POST', body: formData});
    const data = await res.json();
    alert(data.output || data.error);
    loadValues();
}

async function loadValues() {
    const data = await apiCall('values/list');
    const el = document.getElementById('chartValues');
    if (el) {
        el.innerHTML = '<option value="">None</option>' +
            (data.values || []).map(v => `<option value="${v}">${v}</option>`).join('');
    }
}

async function uploadCert() {
    const file = document.getElementById('certFile').files[0];
    if (!file) return alert('Select a certificate');
    
    const formData = new FormData();
    formData.append('cert', file);
    
    const res = await fetch('/api/cert/upload', { method: 'POST', body: formData });
    const data = await res.json();
    alert(data.output || data.error);
}

async function startServe() {
    const port = document.getElementById('servePort').value;
    const mode = document.getElementById('serveMode')?.value || 'registry';
    const readonly = document.getElementById('serveReadonly')?.checked !== false;
    const tlsCert = document.getElementById('serveTLSCert')?.value || '';
    const tlsKey = document.getElementById('serveTLSKey')?.value || '';
    const timeout = parseInt(document.getElementById('serveTimeout')?.value) || 0;
    const config = document.getElementById('serveConfig')?.value || '';
    
    const data = await apiCall('serve/start', 'POST', { port, mode, readonly, tlsCert, tlsKey, timeout, config });
    document.getElementById('serveOutput').textContent = data.output || data.error;
    updateServerStatus();
}

async function stopServe() {
    const data = await apiCall('serve/stop', 'POST');
    document.getElementById('serveOutput').textContent = data.output || data.error;
    updateServerStatus();
}

async function updateServerStatus() {
    const data = await apiCall('serve/status');
    const statusEl = document.getElementById('serverStatus');
    statusEl.textContent = data.running ? 'Running' : 'Stopped';
    statusEl.className = data.running ? 'text-2xl font-bold text-green-400' : 'text-2xl font-bold text-gray-400';
}

function connectWebSocket() {
    if (ws) return;
    ws = new WebSocket(`ws://${location.host}/api/logs`);
    ws.onmessage = (e) => {
        document.getElementById('logOutput').textContent += e.data + '\n';
    };
}

function clearLogs() {
    document.getElementById('logOutput').textContent = '';
}

setInterval(updateServerStatus, 5000);
refreshStoreInfo();
updateServerStatus();
loadRepositories();
loadRegistries();
loadKeys();
loadTLSCerts();
loadValues();
