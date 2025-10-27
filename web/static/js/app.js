(() => {
  'use strict';

  const csrfToken = document.querySelector('meta[name="csrf-token"]')?.content || '';
  const state = {
    hosts: [],
    selectedHostId: null,
    scanning: {},
    portData: {},
    filters: {
      search: '',
      sort: 'number',
      order: 'asc',
      page: 1,
      pageSize: 24,
      status: 'open',
    },
    searchTimer: null,
    eventSource: null,
  };

  const elements = {
    hostList: document.getElementById('host-list'),
    hostTitle: document.getElementById('host-title'),
    hostAddress: document.getElementById('host-address'),
    portGridVisible: document.getElementById('port-grid-visible'),
    portGridHidden: document.getElementById('port-grid-hidden'),
    tabs: document.querySelectorAll('.tab'),
    scanButton: document.getElementById('btn-scan-now'),
    addHost: document.getElementById('btn-add-host'),
    addPort: document.getElementById('btn-add-port'),
    bulkHide: document.getElementById('btn-bulk-hide'),
    bulkDelete: document.getElementById('btn-bulk-delete'),
    showHidden: document.getElementById('btn-show-hidden'),
    deleteHost: document.getElementById('btn-delete-host'),
    unusedPort: document.getElementById('btn-unused-port'),
    unusedDisplay: document.getElementById('unused-port-display'),
    searchInput: document.getElementById('port-search'),
    sortSelect: document.getElementById('port-sort'),
    pageSizeSelect: document.getElementById('page-size'),
    prevPage: document.getElementById('btn-prev-page'),
    nextPage: document.getElementById('btn-next-page'),
    paginationInfo: document.getElementById('pagination-info'),
    modalBackdrop: document.getElementById('modal-backdrop'),
    modalContainer: document.getElementById('modal-container'),
    toastContainer: document.getElementById('toast-container'),
  };

  function fetchJSON(url, options = {}) {
    const init = {
      headers: {
        Accept: 'application/json',
      },
      ...options,
    };
    if (init.method && init.method !== 'GET') {
      init.headers['Content-Type'] = init.headers['Content-Type'] || 'application/json';
      init.headers['X-CSRF-Token'] = csrfToken;
    }
    return fetch(url, init).then(async (res) => {
      if (!res.ok) {
        let message = res.statusText;
        try {
          const payload = await res.json();
          if (payload?.error) {
            message = payload.error;
          }
        } catch (err) {
          (void err);
        }
        throw new Error(message || '请求失败');
      }
      try {
        return await res.json();
      } catch (err) {
        (void err);
        return {};
      }
    });
  }

  function showToast(message, type = 'info', duration = 3200) {
    if (!elements.toastContainer) {
      return;
    }
    const toast = document.createElement('div');
    toast.className = `toast${type === 'success' ? ' toast-success' : ''}${type === 'error' ? ' toast-error' : ''}`;
    toast.textContent = message;
    elements.toastContainer.appendChild(toast);
    setTimeout(() => {
      toast.remove();
    }, duration);
  }

  function notify(message, type = 'error') {
    console.warn(message);
    showToast(message, type);
  }

  function openModal(content) {
    if (!elements.modalBackdrop || !elements.modalContainer) {
      return;
    }
    elements.modalContainer.innerHTML = '';
    elements.modalContainer.appendChild(content);
    elements.modalBackdrop.classList.remove('hidden');
  }

  function closeModal() {
    if (!elements.modalBackdrop || !elements.modalContainer) {
      return;
    }
    elements.modalContainer.innerHTML = '';
    elements.modalBackdrop.classList.add('hidden');
  }

  function confirmModal(message, confirmLabel = '确认', cancelLabel = '取消') {
    return new Promise((resolve) => {
      const wrapper = document.createElement('div');
      wrapper.className = 'modal';
      wrapper.innerHTML = `
        <h2>确认操作</h2>
        <p>${escapeHTML(message)}</p>
        <div class="actions">
          <button class="btn-secondary" data-action="cancel">${escapeHTML(cancelLabel)}</button>
          <button class="btn-danger" data-action="confirm">${escapeHTML(confirmLabel)}</button>
        </div>
      `;
      wrapper.querySelector('[data-action="cancel"]')?.addEventListener('click', () => {
        closeModal();
        resolve(false);
      });
      wrapper.querySelector('[data-action="confirm"]')?.addEventListener('click', () => {
        closeModal();
        resolve(true);
      });
      openModal(wrapper);
    });
  }

  function debounce(fn, delay = 300) {
    return (...args) => {
      clearTimeout(state.searchTimer);
      state.searchTimer = window.setTimeout(() => fn(...args), delay);
    };
  }

  function escapeHTML(str) {
    if (typeof str !== 'string') {
      return '';
    }
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function statusLabel(status) {
    switch ((status || '').toLowerCase()) {
      case 'open':
        return '开放';
      case 'closed':
        return '关闭';
      case 'unknown':
      default:
        return '未知';
    }
  }

  function formatTimestamp(ts) {
    if (!ts) {
      return '未检查';
    }
    try {
      return new Date(ts).toLocaleString();
    } catch (err) {
      (void err);
      return ts;
    }
  }

  function init() {
    attachEventHandlers();
    setActiveTab('visible');
    loadHosts();
    initEventStream();
  }

  function attachEventHandlers() {
    if (elements.addHost) {
      elements.addHost.addEventListener('click', () => openHostForm());
    }
    if (elements.scanButton) {
      elements.scanButton.addEventListener('click', () => triggerScan());
    }
    if (elements.addPort) {
      elements.addPort.addEventListener('click', () => openPortForm());
    }
    if (elements.bulkHide) {
      elements.bulkHide.addEventListener('click', () => openBulkHideModal());
    }
    if (elements.bulkDelete) {
      elements.bulkDelete.addEventListener('click', () => openBulkDeleteModal());
    }
    if (elements.showHidden) {
      elements.showHidden.addEventListener('click', () => setActiveTab('hidden'));
    }
    if (elements.deleteHost) {
      elements.deleteHost.addEventListener('click', () => deleteCurrentHost());
    }
    if (elements.unusedPort) {
      elements.unusedPort.addEventListener('click', () => requestUnusedPort());
    }
    elements.tabs?.forEach((tab) => {
      tab.addEventListener('click', () => setActiveTab(tab.dataset.tab || 'visible'));
    });
    if (elements.searchInput) {
      const handler = debounce((value) => {
        state.filters.search = value.trim();
        state.filters.page = 1;
        if (state.selectedHostId) {
          loadPorts(state.selectedHostId);
        }
      }, 320);
      elements.searchInput.addEventListener('input', (event) => {
        handler(event.target.value);
      });
    }
    if (elements.sortSelect) {
      elements.sortSelect.addEventListener('change', (event) => {
        const value = event.target.value;
        const [sort, ord] = value.split(':');
        state.filters.sort = sort || 'number';
        state.filters.order = ord || 'asc';
        state.filters.page = 1;
        if (state.selectedHostId) {
          loadPorts(state.selectedHostId);
        }
      });
    }
    if (elements.pageSizeSelect) {
      elements.pageSizeSelect.addEventListener('change', (event) => {
        const pageSize = parseInt(event.target.value, 10);
        state.filters.pageSize = Number.isNaN(pageSize) ? 24 : pageSize;
        state.filters.page = 1;
        if (state.selectedHostId) {
          loadPorts(state.selectedHostId);
        }
      });
    }
    if (elements.prevPage) {
      elements.prevPage.addEventListener('click', () => {
        if (state.filters.page > 1) {
          state.filters.page -= 1;
          if (state.selectedHostId) {
            loadPorts(state.selectedHostId);
          }
        }
      });
    }
    if (elements.nextPage) {
      elements.nextPage.addEventListener('click', () => {
        state.filters.page += 1;
        if (state.selectedHostId) {
          loadPorts(state.selectedHostId);
        }
      });
    }
    elements.modalBackdrop?.addEventListener('click', (event) => {
      if (event.target === elements.modalBackdrop) {
        closeModal();
      }
    });
  }

  function initEventStream() {
    if (!window.EventSource) {
      return;
    }
    try {
      const es = new EventSource('/api/events');
      state.eventSource = es;
      es.onmessage = (event) => handleEvent(event.data);
      es.onerror = () => {
        es.close();
        state.eventSource = null;
        setTimeout(initEventStream, 5000);
      };
    } catch (err) {
      console.error('event stream error', err);
    }
  }

  function handleEvent(raw) {
    if (!raw) {
      return;
    }
    let data;
    try {
      data = JSON.parse(raw);
    } catch (err) {
      console.error('invalid event payload', err);
      return;
    }
    const type = data?.type;
    switch (type) {
      case 'host_created':
      case 'host_updated':
      case 'host_deleted':
        loadHosts();
        break;
      case 'host_scan_started':
        setHostScanning(data.hostId, true);
        break;
      case 'host_scanned':
        setHostScanning(data.hostId, false);
        if (data.hostId === state.selectedHostId) {
          showToast('扫描完成', data.payload?.success ? 'success' : 'info');
          loadPorts(state.selectedHostId);
        }
        break;
      case 'port_created':
      case 'port_updated':
      case 'port_deleted':
      case 'port_hidden':
      case 'port_visible':
      case 'ports_hidden':
      case 'ports_unhidden':
      case 'ports_deleted':
      case 'port_status':
        if (state.selectedHostId) {
          loadPorts(state.selectedHostId);
        }
        break;
      default:
        break;
    }
  }

  function setHostScanning(hostId, scanning) {
    if (!hostId) {
      return;
    }
    state.scanning[hostId] = Boolean(scanning);
    const host = state.hosts.find((item) => item.id === hostId);
    if (host) {
      host.scanning = Boolean(scanning);
      renderHosts();
      if (state.selectedHostId === hostId) {
        updateHostHeader(host);
      }
    }
  }

  function updateScanControls() {
    const scanning = state.selectedHostId ? !!state.scanning[state.selectedHostId] : false;
    if (elements.scanButton) {
      elements.scanButton.disabled = !state.selectedHostId || scanning;
      elements.scanButton.textContent = scanning ? '扫描中...' : '刷新端口';
    }
    if (elements.unusedPort) {
      elements.unusedPort.disabled = !state.selectedHostId || scanning;
    }
    if (elements.addPort) {
      elements.addPort.disabled = !state.selectedHostId;
    }
    if (elements.bulkHide) {
      elements.bulkHide.disabled = !state.selectedHostId;
    }
    if (elements.bulkDelete) {
      elements.bulkDelete.disabled = !state.selectedHostId;
    }
    if (elements.showHidden) {
      elements.showHidden.disabled = !state.selectedHostId;
    }
    if (elements.deleteHost) {
      elements.deleteHost.disabled = !state.selectedHostId;
    }
    updateUnusedDisplay(scanning && state.selectedHostId);
  }

  function updateHostHeader(host) {
    if (elements.hostTitle) {
      elements.hostTitle.textContent = host ? host.name : '请选择主机';
    }
    if (elements.hostAddress) {
      const scanning = host ? !!state.scanning[host.id] : false;
      elements.hostAddress.textContent = host
        ? scanning
          ? `${host.address} · 扫描中...`
          : host.address
        : '';
    }
    updateScanControls();
  }

  function updateUnusedDisplay(show) {
    if (!elements.unusedDisplay) {
      return;
    }
    if (show) {
      elements.unusedDisplay.textContent = '扫描进行中，请稍候...';
      elements.unusedDisplay.classList.remove('hidden');
      elements.unusedDisplay.classList.add('status-running');
      return;
    }
    elements.unusedDisplay.textContent = '';
    elements.unusedDisplay.classList.add('hidden');
    elements.unusedDisplay.classList.remove('status-running');
  }

  function renderHosts() {
    if (!elements.hostList) {
      return;
    }
    elements.hostList.innerHTML = '';
    if (!state.hosts.length) {
      const empty = document.createElement('li');
      empty.className = 'host-item empty';
      empty.textContent = '暂无主机，请新增';
      elements.hostList.appendChild(empty);
      updateHostHeader(null);
      clearPortViews();
      return;
    }
    state.hosts.forEach((host) => {
      const li = document.createElement('li');
      li.className = `host-item${host.id === state.selectedHostId ? ' active' : ''}`;
      li.dataset.hostId = String(host.id);
      const scanningBadge = host.scanning ? '<span class="host-badge">扫描中</span>' : '';
      li.innerHTML = `
        <span class="name">${escapeHTML(host.name)}</span>
        <span class="address">${escapeHTML(host.address)}</span>
        <div class="stats">
          <span>在用 ${host.openCount ?? 0}</span>
          <span>隐藏 ${host.hiddenCount ?? 0}</span>
          ${scanningBadge}
        </div>
      `;
      li.addEventListener('click', () => selectHost(host.id));
      elements.hostList.appendChild(li);
    });
    updateScanControls();
  }

  function clearPortViews() {
    if (elements.portGridVisible) {
      elements.portGridVisible.innerHTML = '';
    }
    if (elements.portGridHidden) {
      elements.portGridHidden.innerHTML = '';
    }
    if (elements.paginationInfo) {
      elements.paginationInfo.textContent = '';
    }
  }

  async function loadHosts() {
    try {
      const hosts = await fetchJSON('/api/hosts');
      state.hosts = Array.isArray(hosts) ? hosts : [];
      state.scanning = {};
      state.hosts.forEach((host) => {
        state.scanning[host.id] = !!host.scanning;
      });
      renderHosts();
      if (!state.hosts.length) {
        state.selectedHostId = null;
        updateHostHeader(null);
        return;
      }
      const selected = state.hosts.find((host) => host.id === state.selectedHostId);
      if (selected) {
        updateHostHeader(selected);
        await loadPorts(selected.id);
      } else {
        await selectHost(state.hosts[0].id);
      }
    } catch (err) {
      notify(err.message || '获取主机列表失败');
    }
  }

  async function selectHost(hostId) {
    state.selectedHostId = hostId;
    state.filters.page = 1;
    state.filters.search = '';
    if (elements.searchInput) {
      elements.searchInput.value = '';
    }
    renderHosts();
    const host = state.hosts.find((item) => item.id === hostId) || null;
    updateHostHeader(host);
    if (elements.portGridVisible) {
      elements.portGridVisible.innerHTML = '<div class="empty-state">正在加载端口信息...</div>';
    }
    if (elements.portGridHidden) {
      elements.portGridHidden.innerHTML = '';
    }
    await loadPorts(hostId);
    setActiveTab('visible');
  }

  function setActiveTab(tabName) {
    elements.tabs?.forEach((tab) => {
      tab.classList.toggle('active', tab.dataset.tab === tabName);
    });
    if (elements.portGridVisible) {
      elements.portGridVisible.classList.toggle('hidden', tabName !== 'visible');
    }
    if (elements.portGridHidden) {
      elements.portGridHidden.classList.toggle('hidden', tabName !== 'hidden');
    }
  }

  async function loadPorts(hostId) {
    if (!hostId) {
      return;
    }
    try {
      const params = new URLSearchParams({
        hidden: '1',
        sort: state.filters.sort,
        order: state.filters.order,
        page: String(state.filters.page),
        pageSize: String(state.filters.pageSize),
        status: state.filters.status,
      });
      if (state.filters.search) {
        params.set('q', state.filters.search);
      }
      const data = await fetchJSON(`/api/hosts/${hostId}/ports?${params.toString()}`);
      const ports = Array.isArray(data?.ports) ? data.ports : [];
      const pagination = data?.pagination || { page: 1, pageSize: ports.length, total: ports.length };
      const totalPages = Math.max(
        1,
        Math.ceil((pagination.total || ports.length) / (pagination.pageSize || state.filters.pageSize || 1))
      );
      if (state.filters.page > totalPages) {
        state.filters.page = totalPages;
        if (pagination.page !== totalPages) {
          await loadPorts(hostId);
          return;
        }
      }
      const visible = ports.filter((port) => !port.hidden);
      const hidden = ports.filter((port) => port.hidden);
      const visibleCount = typeof data?.visibleCount === 'number' ? data.visibleCount : visible.length;
      const hiddenCount = typeof data?.hiddenCount === 'number' ? data.hiddenCount : hidden.length;
      state.portData[hostId] = { visible, hidden, pagination };
      recalcHostStats(hostId, visibleCount, hiddenCount);
      if (state.selectedHostId === hostId) {
        renderPorts();
        renderPagination();
      }
    } catch (err) {
      notify(err.message || '获取端口列表失败');
    }
  }

  function recalcHostStats(hostId, openCount, hiddenCount) {
    const host = state.hosts.find((item) => item.id === hostId);
    if (!host) {
      return;
    }
    host.openCount = openCount;
    host.hiddenCount = hiddenCount;
    renderHosts();
    if (state.selectedHostId === hostId) {
      updateHostHeader(host);
    }
  }

  function renderPorts() {
    const hostId = state.selectedHostId;
    if (!hostId) {
      clearPortViews();
      return;
    }
    const data = state.portData[hostId];
    if (!data) {
      if (elements.portGridVisible) {
        elements.portGridVisible.innerHTML = '<div class="empty-state">暂无端口数据</div>';
      }
      if (elements.portGridHidden) {
        elements.portGridHidden.innerHTML = '';
      }
      return;
    }
    if (elements.portGridVisible) {
      elements.portGridVisible.innerHTML = '';
      if (!data.visible.length) {
        elements.portGridVisible.innerHTML = '<div class="empty-state">该主机暂未检测到正在使用的端口</div>';
      } else {
        data.visible.forEach((port) => {
          elements.portGridVisible.appendChild(createPortCard(port, false));
        });
      }
    }
    if (elements.portGridHidden) {
      elements.portGridHidden.innerHTML = '';
      if (!data.hidden.length) {
        elements.portGridHidden.innerHTML = '<div class="empty-state">暂无隐藏的端口</div>';
      } else {
        data.hidden.forEach((port) => {
          elements.portGridHidden.appendChild(createPortCard(port, true));
        });
      }
    }
  }

  function renderPagination() {
    const hostId = state.selectedHostId;
    const data = hostId ? state.portData[hostId] : null;
    if (!data || !elements.paginationInfo || !elements.prevPage || !elements.nextPage || !elements.pageSizeSelect) {
      return;
    }
    const page = data.pagination?.page ?? state.filters.page;
    const pageSize = data.pagination?.pageSize ?? state.filters.pageSize;
    const total = data.pagination?.total ?? 0;
    const totalPages = Math.max(1, Math.ceil(total / (pageSize || 1)));
    elements.paginationInfo.textContent = `第 ${page} / ${totalPages} 页，共 ${total} 条`;
    elements.prevPage.disabled = page <= 1;
    elements.nextPage.disabled = page >= totalPages;
    elements.pageSizeSelect.value = String(pageSize || state.filters.pageSize);
  }

  function createPortCard(port, hiddenTab) {
    const div = document.createElement('div');
    div.className = `port-card${hiddenTab ? ' port-card-hidden' : ''}`;
    div.dataset.portId = String(port.id);
    const fingerprint = escapeHTML(port.fingerprint || port.note || '未知服务');
    const status = (port.status || 'unknown').toLowerCase();
    const statusClass = `status-${status}`;
    div.innerHTML = `
      <div class="port-card-header">
        <span class="port-fingerprint">${fingerprint}</span>
        <span class="status-chip ${statusClass}">${statusLabel(port.status)}</span>
      </div>
      <div class="port-card-body">
        <span class="port-number">:${port.number}</span>
        <span class="port-last">${escapeHTML(formatTimestamp(port.lastChecked))}</span>
      </div>
      <div class="port-actions">
        <button class="btn-secondary" data-action="edit">编辑</button>
        <button class="btn-secondary" data-action="toggle">${hiddenTab ? '取消隐藏' : '隐藏'}</button>
        <button class="btn-danger" data-action="delete">删除</button>
      </div>
    `;
    div.querySelector('[data-action="edit"]')?.addEventListener('click', () => openEditPortModal(port));
    div.querySelector('[data-action="toggle"]')?.addEventListener('click', () => togglePortHidden(port));
    div.querySelector('[data-action="delete"]')?.addEventListener('click', () => deletePort(port));
    return div;
  }

  function triggerScan() {
    if (!state.selectedHostId) {
      return;
    }
    setHostScanning(state.selectedHostId, true);
    fetchJSON(`/api/hosts/${state.selectedHostId}/scan`, {
      method: 'POST',
      body: JSON.stringify({}),
    })
      .then((res) => {
        if (res?.status === 'scanning') {
          showToast('扫描已在进行中', 'info');
        } else {
          showToast('已提交扫描任务', 'success');
        }
      })
      .catch((err) => {
        setHostScanning(state.selectedHostId, false);
        notify(err.message || '触发扫描失败');
      });
  }

  function openHostForm(host = null) {
    const isEdit = Boolean(host);
    const form = document.createElement('form');
    form.className = 'modal form-modal';
    form.innerHTML = `
      <div class="form-modal-hero">
        <div class="form-modal-hero-icon">🛰️</div>
        <div class="form-modal-hero-text">
          <h2>${isEdit ? '编辑主机信息' : '新增主机'}</h2>
          <p>${isEdit ? '更新主机名称或目标地址，保存后即可重新扫描。' : '填写主机名称与目标地址，创建后将立即进入扫描队列。'}</p>
        </div>
      </div>
      <div class="form-modal-body">
        <div class="form-field">
          <label for="host-name-input">名称</label>
          <input id="host-name-input" type="text" name="name" required value="${escapeHTML(host?.name || '')}" placeholder="办公网络 / 数据中心等描述">
        </div>
        <div class="form-field">
          <label for="host-address-input">目标地址</label>
          <input id="host-address-input" type="text" name="address" required value="${escapeHTML(host?.address || '')}" placeholder="例如：example.com 或 192.168.1.10">
          <span class="form-field-hint">支持域名或 IP，系统会自动解析并用于端口探测。</span>
        </div>
      </div>
      <div class="actions form-modal-actions">
        <button type="button" class="btn-secondary" data-action="cancel">取消</button>
        <button type="submit" class="btn-primary">${isEdit ? '保存修改' : '立即创建'}</button>
      </div>
    `;
    form.querySelector('[data-action="cancel"]')?.addEventListener('click', () => closeModal());
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const formData = new FormData(form);
      const payload = {
        name: formData.get('name')?.toString().trim() || '',
        address: formData.get('address')?.toString().trim() || '',
        autoScan: false,
      };
      if (!payload.name || !payload.address) {
        notify('名称和地址不能为空');
        return;
      }
      try {
        if (isEdit && host) {
          await fetchJSON(`/api/hosts/${host.id}`, {
            method: 'PUT',
            body: JSON.stringify(payload),
          });
          showToast('主机已更新', 'success');
        } else {
          await fetchJSON('/api/hosts', {
            method: 'POST',
            body: JSON.stringify(payload),
          });
          showToast('主机已创建', 'success');
        }
        closeModal();
        await loadHosts();
      } catch (err) {
        notify(err.message || '保存主机失败');
      }
    });
    openModal(form);
  }

  function openPortForm() {
    if (!state.selectedHostId) {
      notify('请选择主机');
      return;
    }
    const form = document.createElement('form');
    form.className = 'modal form-modal';
    form.innerHTML = `
      <h2>新增端口</h2>
      <label>端口号
        <input type="number" name="number" min="1" max="65535" required placeholder="80">
      </label>
      <label>备注
        <input type="text" name="note" placeholder="备注（可选）">
      </label>
      <label>指纹
        <input type="text" name="fingerprint" placeholder="服务指纹（可选）">
      </label>
      <div class="actions">
        <button type="button" class="btn-secondary" data-action="cancel">取消</button>
        <button type="submit" class="btn-primary">添加</button>
      </div>
    `;
    form.querySelector('[data-action="cancel"]')?.addEventListener('click', () => closeModal());
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const formData = new FormData(form);
      const number = parseInt(formData.get('number'), 10);
      if (!number || number < 1 || number > 65535) {
        notify('请输入 1-65535 的端口号');
        return;
      }
      const payload = {
        number,
        note: formData.get('note')?.toString().trim() || '',
        fingerprint: formData.get('fingerprint')?.toString().trim() || '',
      };
      try {
        await fetchJSON(`/api/hosts/${state.selectedHostId}/ports`, {
          method: 'POST',
          body: JSON.stringify(payload),
        });
        closeModal();
        showToast('端口已添加', 'success');
        await loadPorts(state.selectedHostId);
      } catch (err) {
        notify(err.message || '新增端口失败');
      }
    });
    openModal(form);
  }

  function openEditPortModal(port) {
    const form = document.createElement('form');
    form.className = 'modal form-modal';
    form.innerHTML = `
      <h2>编辑端口 :${port.number}</h2>
      <label>备注
        <input type="text" name="note" required value="${escapeHTML(port.note || '')}">
      </label>
      <label>指纹
        <input type="text" name="fingerprint" value="${escapeHTML(port.fingerprint || '')}">
      </label>
      <div class="actions">
        <button type="button" class="btn-secondary" data-action="cancel">取消</button>
        <button type="submit" class="btn-primary">保存</button>
      </div>
    `;
    form.querySelector('[data-action="cancel"]')?.addEventListener('click', () => closeModal());
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const formData = new FormData(form);
      const note = formData.get('note')?.toString().trim() || '';
      const fingerprint = formData.get('fingerprint')?.toString().trim() || '';
      if (!note) {
        notify('备注不能为空');
        return;
      }
      try {
        await fetchJSON(`/api/ports/${port.id}`, {
          method: 'PUT',
          body: JSON.stringify({ note, fingerprint }),
        });
        closeModal();
        showToast('端口已更新', 'success');
        if (state.selectedHostId) {
          await loadPorts(state.selectedHostId);
        }
      } catch (err) {
        notify(err.message || '更新端口失败');
      }
    });
    openModal(form);
  }

  async function togglePortHidden(port) {
    const endpoint = port.hidden ? `/api/ports/${port.id}/unhide` : `/api/ports/${port.id}/hide`;
    try {
      await fetchJSON(endpoint, { method: 'POST', body: JSON.stringify({}) });
      showToast(port.hidden ? '端口已取消隐藏' : '端口已隐藏', 'success');
      if (state.selectedHostId) {
        await loadPorts(state.selectedHostId);
      }
    } catch (err) {
      notify(err.message || '切换端口可见性失败');
    }
  }

  async function deletePort(port) {
    const confirmed = await confirmModal(`确定要删除端口 :${port.number} 吗？`, '删除');
    if (!confirmed) {
      return;
    }
    try {
      await fetchJSON(`/api/ports/${port.id}`, { method: 'DELETE' });
      showToast('端口已删除', 'success');
      if (state.selectedHostId) {
        await loadPorts(state.selectedHostId);
      }
    } catch (err) {
      notify(err.message || '删除端口失败');
    }
  }

  function openBulkHideModal() {
    if (!state.selectedHostId) {
      notify('请选择主机');
      return;
    }
    const form = document.createElement('form');
    form.className = 'modal form-modal';
    form.innerHTML = `
      <h2>批量隐藏/显示端口</h2>
      <label>起始端口
        <input type="number" name="start" min="1" max="65535" required placeholder="1">
      </label>
      <label>结束端口
        <input type="number" name="end" min="1" max="65535" required placeholder="65535">
      </label>
      <label class="checkbox">
        <input type="checkbox" name="hide" checked>
        勾选则隐藏，取消勾选则取消隐藏
      </label>
      <div class="actions">
        <button type="button" class="btn-secondary" data-action="cancel">取消</button>
        <button type="submit" class="btn-primary">执行</button>
      </div>
    `;
    form.querySelector('[data-action="cancel"]')?.addEventListener('click', () => closeModal());
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const formData = new FormData(form);
      const start = parseInt(formData.get('start'), 10);
      const end = parseInt(formData.get('end'), 10);
      if (!start || !end || start > end) {
        notify('请输入有效的端口范围');
        return;
      }
      const payload = {
        start,
        end,
        hide: formData.get('hide') === 'on',
      };
      try {
        await fetchJSON(`/api/hosts/${state.selectedHostId}/ports/bulk_hide`, {
          method: 'POST',
          body: JSON.stringify(payload),
        });
        closeModal();
        showToast('批量操作已完成', 'success');
        await loadPorts(state.selectedHostId);
      } catch (err) {
        notify(err.message || '批量隐藏失败');
      }
    });
    openModal(form);
  }

  function openBulkDeleteModal() {
    if (!state.selectedHostId) {
      notify('请选择主机');
      return;
    }
    const form = document.createElement('form');
    form.className = 'modal form-modal';
    form.innerHTML = `
      <h2>批量删除端口</h2>
      <label>起始端口
        <input type="number" name="start" min="1" max="65535" required placeholder="1">
      </label>
      <label>结束端口
        <input type="number" name="end" min="1" max="65535" required placeholder="65535">
      </label>
      <div class="actions">
        <button type="button" class="btn-secondary" data-action="cancel">取消</button>
        <button type="submit" class="btn-danger">删除</button>
      </div>
    `;
    form.querySelector('[data-action="cancel"]')?.addEventListener('click', () => closeModal());
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      const formData = new FormData(form);
      const start = parseInt(formData.get('start'), 10);
      const end = parseInt(formData.get('end'), 10);
      if (!start || !end || start > end) {
        notify('请输入有效的端口范围');
        return;
      }
      const confirmed = await confirmModal(`确定要删除 ${start}-${end} 范围内的端口吗？`, '删除');
      if (!confirmed) {
        return;
      }
      try {
        await fetchJSON(`/api/hosts/${state.selectedHostId}/ports/bulk_delete`, {
          method: 'POST',
          body: JSON.stringify({ start, end }),
        });
        closeModal();
        showToast('批量删除已完成', 'success');
        await loadPorts(state.selectedHostId);
      } catch (err) {
        notify(err.message || '批量删除失败');
      }
    });
    openModal(form);
  }

  async function deleteCurrentHost() {
    if (!state.selectedHostId) {
      notify('请选择主机');
      return;
    }
    const host = state.hosts.find((item) => item.id === state.selectedHostId);
    const confirmed = await confirmModal(
      `确定要删除主机 ${host ? host.name : ''} 以及其所有端口吗？`,
      '删除'
    );
    if (!confirmed) {
      return;
    }
    try {
      await fetchJSON(`/api/hosts/${state.selectedHostId}`, { method: 'DELETE' });
      showToast('主机已删除', 'success');
      state.selectedHostId = null;
      await loadHosts();
    } catch (err) {
      notify(err.message || '删除主机失败');
    }
  }

  async function requestUnusedPort() {
    if (!state.selectedHostId) {
      notify('请选择主机');
      return;
    }
    const button = elements.unusedPort;
    let originalLabel = '';
    if (button) {
      originalLabel = button.textContent || '';
      button.disabled = true;
      button.textContent = '获取中...';
    }
    try {
      const data = await fetchJSON(`/api/hosts/${state.selectedHostId}/unused_port`);
      showUnusedPortModal(data.port);
    } catch (err) {
      notify(err.message || '未找到可用端口');
    } finally {
      if (button) {
        const scanning = state.selectedHostId ? !!state.scanning[state.selectedHostId] : false;
        button.disabled = !state.selectedHostId || scanning;
        button.textContent = originalLabel || '获取未使用端口';
      }
    }
  }

  function showUnusedPortModal(portNumber) {
    const safePort = portNumber ?? '未知';
    const copyValue = String(portNumber ?? '');
    const modal = document.createElement('div');
    modal.className = 'modal';
    modal.innerHTML = `
      <h2>推荐未使用端口</h2>
      <p class="modal-hint">系统已根据该主机当前端口占用情况自动挑选一个可用端口。</p>
      <div class="unused-port-modal">
        <span class="unused-port-label">建议使用</span>
        <span class="unused-port-value">:${safePort}</span>
      </div>
      <div class="actions">
        <button type="button" class="btn-secondary" data-action="close">关闭</button>
        <button type="button" class="btn-primary" data-action="copy">复制端口</button>
      </div>
    `;
    modal.querySelector('[data-action="close"]')?.addEventListener('click', () => closeModal());
    modal.querySelector('[data-action="copy"]')?.addEventListener('click', async () => {
      if (!copyValue) {
        notify('端口信息不可用');
        return;
      }
      const text = copyValue;
      try {
        if (navigator.clipboard && navigator.clipboard.writeText) {
          await navigator.clipboard.writeText(text);
          showToast('端口号已复制', 'success');
        } else if (fallbackCopy(text)) {
          showToast('端口号已复制', 'success');
        } else {
          notify('复制失败，请手动复制');
        }
      } catch (err) {
        if (fallbackCopy(text)) {
          showToast('端口号已复制', 'success');
        } else {
          notify('复制失败，请手动复制');
        }
      }
    });
    openModal(modal);
  }

  function fallbackCopy(text) {
    const input = document.createElement('input');
    input.value = text;
    document.body.appendChild(input);
    input.select();
    try {
      const success = document.execCommand('copy');
      document.body.removeChild(input);
      return success;
    } catch (err) {
      document.body.removeChild(input);
      return false;
    }
  }

  init();
})();
