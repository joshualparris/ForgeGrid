const connectionDot = document.getElementById('connection-dot');
const connectionStatus = document.getElementById('connection-status');
const nodeNameEl = document.getElementById('node-name');
const workerIdEl = document.getElementById('worker-id');
const coordinatorUrlEl = document.getElementById('coordinator-url');

const cpuCoresEl = document.getElementById('cpu-cores');
const cpuModelEl = document.getElementById('cpu-model');
const ramUsageEl = document.getElementById('ram-usage');
const ramBar = document.getElementById('ram-bar');
const ramText = document.getElementById('ram-text');
const diskFreeEl = document.getElementById('disk-free');
const osArchEl = document.getElementById('os-arch');

const jobCountEl = document.getElementById('job-count');
const jobsListEl = document.getElementById('jobs-list');

function formatBytes(bytes, decimals = 2) {
    if (!+bytes) return '0 Bytes';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`;
}

async function fetchStatus() {
    try {
        const res = await fetch('/api/status');
        if (!res.ok) throw new Error('API Error');
        const data = await res.json();
        
        // Update connection status
        connectionDot.className = 'dot green';
        connectionStatus.textContent = 'Online';
        
        // Update identity
        nodeNameEl.textContent = data.node_name || 'Unknown';
        workerIdEl.textContent = data.worker_id || 'Not paired';
        coordinatorUrlEl.textContent = data.coordinator_url || 'N/A';
        
        const hw = data.hardware;
        if (hw) {
            cpuCoresEl.textContent = hw.logical_processors || 0;
            cpuModelEl.textContent = hw.cpu_model || 'Unknown CPU';
            osArchEl.textContent = `${hw.os} ${hw.architecture} (${hw.os_version})`;
            
            diskFreeEl.textContent = formatBytes(hw.free_workspace_disk);
            
            // RAM calculation
            const totalRAM = hw.total_ram;
            const availRAM = hw.available_ram;
            const usedRAM = totalRAM - availRAM;
            const ramPct = (usedRAM / totalRAM) * 100;
            
            ramUsageEl.textContent = `${ramPct.toFixed(1)}%`;
            ramBar.style.width = `${ramPct}%`;
            
            if (ramPct > 85) ramBar.className = 'progress-fill critical';
            else if (ramPct > 70) ramBar.className = 'progress-fill high';
            else ramBar.className = 'progress-fill';
            
            ramText.textContent = `${formatBytes(usedRAM)} / ${formatBytes(totalRAM)}`;
        }
        
        // Update jobs
        const jobs = data.active_jobs || [];
        jobCountEl.textContent = jobs.length;
        
        if (jobs.length === 0) {
            jobsListEl.innerHTML = '<div class="loading">No active jobs. Waiting for coordinator...</div>';
        } else {
            let html = '';
            jobs.forEach(jobId => {
                html += `
                    <div class="job-card">
                        <div class="job-header">
                            <span class="job-id">${jobId}</span>
                            <span class="badge running">Running</span>
                        </div>
                        <div class="job-meta">
                            <span><svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><polyline points="12 6 12 12 16 14"></polyline></svg> Executing in secure workspace</span>
                        </div>
                    </div>
                `;
            });
            jobsListEl.innerHTML = html;
        }

        const recentJobs = data.recent_jobs || [];
        const recentJobsListEl = document.getElementById('recent-jobs-list');
        if (recentJobs.length === 0) {
            recentJobsListEl.innerHTML = '<div class="loading" style="padding: 10px; font-size: 0.85rem;">No recent jobs</div>';
        } else {
            let recentHtml = '';
            // reverse to show newest first
            [...recentJobs].reverse().forEach(jobId => {
                recentHtml += `
                    <div class="job-card" style="opacity: 0.8;">
                        <div class="job-header" style="margin-bottom: 0;">
                            <span class="job-id">${jobId}</span>
                            <span class="badge" style="background: rgba(16, 185, 129, 0.15); color: var(--success);">Completed</span>
                        </div>
                    </div>
                `;
            });
            recentJobsListEl.innerHTML = recentHtml;
        }
        
    } catch (err) {
        connectionDot.className = 'dot red';
        connectionStatus.textContent = 'Offline (API Unreachable)';
        console.error(err);
    }
}

// Poll every 2 seconds
fetchStatus();
setInterval(fetchStatus, 2000);
