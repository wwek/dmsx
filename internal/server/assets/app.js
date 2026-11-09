const nav = [
    ["overview", "总览"],
    ["databases", "数据库"],
    ["storages", "存储"],
    ["backups", "整库备份"],
    ["archive", "单表归档"],
    ["cold", "冷查询"],
    ["ddl", "无感变更"],
    ["notifiers", "通知"],
    ["audit", "审计"],
];

const icons = {
    overview: `<svg viewBox="0 0 24 24"><rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.5"/></svg>`,
    databases: `<svg viewBox="0 0 24 24"><ellipse cx="12" cy="6" rx="7" ry="3"/><path d="M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6"/><path d="M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6"/></svg>`,
    storages: `<svg viewBox="0 0 24 24"><path d="M4 8l8-4 8 4-8 4-8-4z"/><path d="M4 8v8l8 4 8-4V8"/><path d="M12 12v8"/></svg>`,
    backups: `<svg viewBox="0 0 24 24"><path d="M12 4v10"/><path d="M8 10l4 4 4-4"/><path d="M5 19h14"/></svg>`,
    archive: `<svg viewBox="0 0 24 24"><path d="M4 7h16v4H4z"/><path d="M6 11v8h12v-8"/><path d="M10 15h4"/></svg>`,
    cold: `<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="6"/><path d="M16 16l4 4"/></svg>`,
    ddl: `<svg viewBox="0 0 24 24"><path d="M4 20h4l10-10-4-4L4 16v4z"/><path d="M13 7l4 4"/></svg>`,
    notifiers: `<svg viewBox="0 0 24 24"><path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15L6 16z"/><path d="M10 20a2 2 0 0 0 4 0"/></svg>`,
    audit: `<svg viewBox="0 0 24 24"><path d="M7 4h10v16H7z"/><path d="M10 8h4M10 12h4M10 16h2"/></svg>`,
};

const storageFields = {
    LOCAL: [["path", "目录", "text"]],
    S3: [["bucket", "Bucket", "text"], ["region", "Region", "text"], ["endpoint", "Endpoint", "text"], ["accessKey", "Access Key", "text"], ["secretKey", "Secret Key", "password"], ["prefix", "前缀", "text"], ["storageClass", "Storage class", "text"]],
    QINIU: [["bucket", "Bucket", "text"], ["region", "Region，如 cn-east-1", "text"], ["endpoint", "Endpoint，可留空", "text"], ["accessKey", "Access Key", "text"], ["secretKey", "Secret Key", "password"], ["prefix", "备份前缀", "text"]],
    SFTP: [["host", "主机", "text"], ["port", "端口", "number"], ["user", "用户", "text"], ["password", "密码", "password"], ["key", "私钥", "textarea"], ["prefix", "前缀", "text"]],
    FTP: [["host", "主机", "text"], ["port", "端口", "number"], ["user", "用户", "text"], ["password", "密码", "password"], ["prefix", "前缀", "text"]],
    AZURE_BLOB: [["account", "账号", "text"], ["accountKey", "Account key", "password"], ["container", "容器", "text"], ["endpoint", "Endpoint", "text"], ["prefix", "前缀", "text"]],
    GOOGLE_DRIVE: [["accessToken", "Access token", "password"], ["folderId", "Folder ID", "text"]],
    NAS: [["host", "主机", "text"], ["port", "端口", "number"], ["share", "共享名", "text"], ["user", "用户", "text"], ["password", "密码", "password"], ["domain", "域", "text"], ["prefix", "前缀", "text"]],
    RCLONE: [["remote", "远端，如 qiniu:bucket", "text"], ["bin", "rclone 路径", "text"]],
};

const notifierFields = {
    EMAIL: [["host", "SMTP 主机", "text"], ["port", "端口，可留空", "number"], ["security", "加密", "select:STARTTLS,TLS,NONE"], ["user", "用户", "text"], ["password", "密码", "password"], ["from", "发件人，可留空", "text"], ["to", "收件人，逗号分隔", "text"], ["insecureSkipVerify", "不校验服务器证书", "checkbox"]],
    TELEGRAM: [["botToken", "Bot token", "password"], ["chatId", "Chat ID", "text"], ["threadId", "话题 ID，可留空", "number"], ["apiUrl", "Bot API 地址，可留空", "text"]],
    SLACK: [["botToken", "Bot token", "password"], ["channel", "频道 ID", "text"], ["webhookUrl", "或者填 Incoming webhook", "password"]],
    DISCORD: [["webhookUrl", "Webhook URL", "password"]],
    TEAMS: [["webhookUrl", "Power Automate 工作流 URL", "password"]],
    MATTERMOST: [["webhookUrl", "Incoming webhook", "password"], ["channel", "频道，可留空", "text"], ["username", "显示名，可留空", "text"], ["serverUrl", "或者走 Bot：服务器地址", "text"], ["botToken", "Bot token", "password"], ["channelId", "频道 ID", "text"]],
    WEBHOOK: [["url", "URL", "text"], ["method", "方法", "select:POST,GET"], ["headers", "请求头，每行 Name: value", "textarea"], ["bodyTemplate", "Body 模板，可用 {{event}} {{heading}} {{message}}", "textarea"]],
};

const eventLabels = {
    BACKUP_SUCCESS: "备份成功",
    BACKUP_FAILED: "备份失败",
    HEALTH_UNAVAILABLE: "数据库不可用",
    HEALTH_AVAILABLE: "数据库恢复",
    RESTORE_SUCCESS: "恢复成功",
    RESTORE_FAILED: "恢复失败",
};
const defaultEvents = ["BACKUP_FAILED", "HEALTH_UNAVAILABLE", "HEALTH_AVAILABLE", "RESTORE_FAILED"];

const auditLabels = {
    "session.open": "登录",
    "session.refused": "口令错误",
    "session.close": "退出",
    "operator.setup": "设置口令",
    "operator.reset": "命令行重置口令",
    "database.create": "登记数据库",
    "database.delete": "删除数据库",
    "database.backup_user": "创建备份用户",
    "database.healthcheck": "修改健康检查",
    "storage.create": "新增存储",
    "storage.delete": "删除存储",
    "backup_plan.create": "新建备份计划",
    "backup_plan.delete": "删除备份计划",
    "backup.run": "立即备份",
    "backup.delete": "删除备份",
    "restore.start": "开始恢复",
    "archive.start": "开始归档",
    "ddl.start": "开始结构变更",
    "job.cancel": "取消任务",
    "notifier.create": "新增通知",
    "notifier.delete": "删除通知",
};

const state = { section: "overview", databases: [], storages: [], configs: [], runs: [], restores: [], archives: [], ddls: [], notifiers: [], health: [], audit: [] };

const $ = (id) => document.getElementById(id);

async function api(path, options = {}) {
    const response = await fetch(path, {
        headers: { "Content-Type": "application/json", ...(options.headers || {}) },
        ...options,
    });
    const text = await response.text();
    const body = text ? JSON.parse(text) : {};
    if (!response.ok) {
        const error = new Error(body.error || response.statusText);
        error.status = response.status;
        throw error;
    }
    return body;
}

function esc(value) {
    return String(value ?? "").replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch]));
}

function renderNav() {
    $("nav").innerHTML = nav.map(([id, label]) => `<button data-section="${id}" class="${state.section === id ? "active" : ""}" title="${esc(label)}">${icons[id]}<span>${label}</span></button>`).join("");
    $("nav").onclick = (event) => {
        const button = event.target.closest("button");
        if (!button) return;
        state.section = button.dataset.section;
        draw();
    };
}

async function refresh() {
    [state.databases, state.storages, state.configs, state.runs, state.restores, state.archives, state.ddls, state.notifiers, state.health, state.audit] = await Promise.all([
        api("/api/databases"), api("/api/storages"), api("/api/backups/configs"),
        api("/api/backups/runs"), api("/api/backups/restores"), api("/api/archive/jobs"), api("/api/ddl/runs"),
        api("/api/notifiers"), api("/api/healthchecks"), api("/api/audit"),
    ]);
}

function when(value) {
    if (!value || value.startsWith("0001-")) return "—";
    return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

function size(bytes) {
    if (!bytes) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    let value = bytes;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
    return `${value.toFixed(unit ? 1 : 0)} ${units[unit]}`;
}

function field(name, label, kind) {
    if (kind === "textarea") return `<label>${label}<textarea name="${name}"></textarea></label>`;
    if (kind === "checkbox") return `<label class="check"><input type="checkbox" name="${name}"><span>${label}</span></label>`;
    if (kind.startsWith("select:")) return `<label>${label}<select name="${name}">${kind.slice(7).split(",").map((item) => `<option>${item}</option>`).join("")}</select></label>`;
    return `<label>${label}<input name="${name}" type="${kind}"></label>`;
}

function collect(form, fields) {
    const settings = {};
    for (const [name, , kind] of fields) {
        const input = form.elements[name];
        if (!input) continue;
        if (kind === "checkbox") { if (input.checked) settings[name] = true; continue; }
        if (input.value === "") continue;
        settings[name] = kind === "number" ? Number(input.value) : input.value;
    }
    return settings;
}

function healthOf(id) {
    return state.health.find((row) => row.databaseId === id) || { status: "", attempts: [] };
}

function healthBadge(db) {
    const config = db.healthCheck || { enabled: true };
    if (!config.enabled) return `<span class="badge">未开启探测</span>`;
    const health = healthOf(db.id);
    if (health.status === "AVAILABLE") return `<span class="badge ok">可用</span>`;
    if (health.status === "UNAVAILABLE") return `<span class="badge bad">不可用</span>`;
    const failures = health.attempts.filter((attempt) => attempt.status === "UNAVAILABLE").length;
    if (health.attempts.length) return `<span class="badge">探测中${failures ? `，已失败 ${failures} 次` : ""}</span>`;
    return `<span class="badge">未探测</span>`;
}

function options(rows, label) {
    if (!rows.length) return `<option value="">还没有${label}</option>`;
    return rows.map((row) => `<option value="${esc(row.id)}">${esc(row.name)}</option>`).join("");
}

function draw() {
    renderNav();
    const view = $("view");
    const titles = Object.fromEntries(nav);
    $("crumb").textContent = titles[state.section];
    view.innerHTML = views[state.section]();
    bind(view);
}

const views = {
    overview() {
        const down = state.databases.filter((db) => healthOf(db.id).status === "UNAVAILABLE");
        const health = state.databases.length ? `<div class="list" style="margin-top:22px">${state.databases.map((db) => `<article><div class="row"><strong>${esc(db.name)}</strong>${healthBadge(db)}</div><span class="muted">最近探测 ${when(healthOf(db.id).checkedAt)}</span></article>`).join("")}</div>` : "";
        return `
            ${down.length ? `<p class="alert">${down.length} 个数据库探测不通：${down.map((db) => esc(db.name)).join("、")}</p>` : ""}
            <h1>把库从磁盘上挪走。</h1>
            <p class="muted">导出工具的标准输出直接进压缩和对象存储。归档走内存里的 Parquet。结构变更交给 gh-ost，不在主表上硬改。</p>
            <div class="grid" style="margin-top:22px">
                <article class="card"><strong>整库流式备份</strong><p>MySQL、MariaDB、PostgreSQL 逻辑与物理、MongoDB。管道是原生导出、zstd、可选 AES-GCM，再按 16 MiB 一块上传。S3、七牛、本地、SFTP、FTP、Azure、Drive、NAS、rclone 都在同一条写路径上。</p></article>
                <article class="card"><strong>单表归档与冷查</strong><p>游标读出的行在内存里写成 Parquet，由 S3 Uploader 推到 dms_archives/库/表/年份_archive.parquet。回读校验 MD5 之后，才按 5000 行一批删除。DuckDB 用 httpfs 做通配查询。</p></article>
                <article class="card"><strong>无感结构变更</strong><p>gh-ost 建影子表、分批拷贝、伪装成从库追 binlog，最后 RENAME 切表。进度从它的标准输出里拆出来，经 WebSocket 画成进度条。</p></article>
                <article class="card"><strong>探测与通知</strong><p>每个库按自己的间隔探测连通性，连续失败才判为不可用。备份、恢复和探测状态的变化可以发到邮件、Telegram、Slack、Discord、Teams、Mattermost 或 Webhook。</p></article>
            </div>
            ${health}`;
    },
    databases() {
        return `<div class="split">
            <form id="db-form">
                <h2>登记实例</h2>
                <label>名称<input name="name" required></label>
                <label>引擎<select name="engine"><option>mysql</option><option>mariadb</option><option>postgresql</option><option>mongodb</option></select></label>
                <label>主机<input name="host" value="127.0.0.1" required></label>
                <label>端口<input name="port" type="number" value="3306" required></label>
                <label>用户<input name="username" required></label>
                <label>密码<input name="password" type="password"></label>
                <label>数据库<input name="database" required></label>
                <label>版本提示<input name="version" placeholder="8.0 或 16"></label>
                <label class="check"><input type="checkbox" name="ssl"><span>要求 TLS</span></label>
                <label class="check"><input type="checkbox" name="includeTriggers"><span>包含触发器</span></label>
                <label class="check"><input type="checkbox" name="includeEvents"><span>包含事件</span></label>
                <h2>SSH 跳板，可选</h2>
                <label>主机<input name="sshHost"></label>
                <label>端口<input name="sshPort" type="number" value="22"></label>
                <label>用户<input name="sshUser"></label>
                <label>密码<input name="sshPassword" type="password"></label>
                <button class="primary" type="submit">保存</button>
            </form>
            <div class="list">${state.databases.map(dbCard).join("") || `<p class="muted">还没有数据库。</p>`}</div>
        </div>`;
    },
    storages() {
        const type = state.storageType || "QINIU";
        const fields = (storageFields[type] || []).map(([name, label, kind]) => {
            if (kind === "textarea") return `<label>${label}<textarea name="${name}"></textarea></label>`;
            return `<label>${label}<input name="${name}" type="${kind}"></label>`;
        }).join("");
        return `<div class="split">
            <form id="storage-form">
                <h2>存储目标</h2>
                <label>名称<input name="name" required></label>
                <label>类型<select name="type">${Object.keys(storageFields).map((item) => `<option ${item === type ? "selected" : ""}>${item}</option>`).join("")}</select></label>
                ${fields}
                <p class="muted">整库备份按 Databasus 的方式分块。归档到 S3 / 七牛时改走 Uploader，保证 DuckDB 能按对象读取。</p>
                <button class="primary" type="submit">保存</button>
            </form>
            <div class="list">${state.storages.map(storageCard).join("") || `<p class="muted">还没有存储。</p>`}</div>
        </div>`;
    },
    backups() {
        return `<div class="split">
            <form id="backup-form">
                <h2>备份计划</h2>
                <label>名称<input name="name" required></label>
                <label>数据库<select name="databaseId">${options(state.databases, "数据库")}</select></label>
                <label>存储<select name="storageId">${options(state.storages, "存储")}</select></label>
                <label>方式<select name="method"><option value="logical">逻辑导出</option><option value="physical">PostgreSQL 物理流</option></select></label>
                <label>调度<select name="scheduleType"><option value="DAILY">每天</option><option value="HOURLY">每小时</option><option value="WEEKLY">每周</option><option value="MONTHLY">每月</option><option value="CRON">Cron</option></select></label>
                <label>时刻或 Cron<input name="timeOfDay" value="02:00" placeholder="HH:MM，每周用 1–7，Cron 写表达式"></label>
                <label>排除表<input name="excludeTables" placeholder="logs, tmp_*"></label>
                <label>保留<select name="retention"><option value="COUNT">保留最近份数</option><option value="TIME_PERIOD">按时间</option><option value="GFS">GFS</option></select></label>
                <label>份数或天数<input name="count" type="number" value="7"></label>
                <label>单个备份上限 MB，0 不限<input name="maxBackupSizeMb" type="number" min="0" value="0"></label>
                <label>本计划总量上限 MB，0 不限<input name="maxTotalSizeMb" type="number" min="0" value="0"></label>
                <label class="check"><input type="checkbox" name="encrypted"><span>AES-GCM 加密后上传</span></label>
                <button class="primary">保存计划</button>
            </form>
            <div>
                <div class="list">${state.configs.map(configCard).join("") || `<p class="muted">还没有计划。</p>`}</div>
                <h2 style="margin-top:22px">运行</h2>
                <div class="list">${state.runs.map(runCard).join("") || `<p class="muted">还没有运行记录。</p>`}</div>
                <h2 style="margin-top:22px">恢复</h2>
                <form id="restore-form" class="card">
                    <label>备份<select name="backupId">${runOptions(state.runs)}</select></label>
                    <label>主机<input name="host" required placeholder="127.0.0.1"></label>
                    <label>端口<input name="port" type="number" required placeholder="3306"></label>
                    <label>用户<input name="username" required></label>
                    <label>口令<input name="password" type="password"></label>
                    <label>库名<input name="database" required></label>
                    <p class="muted">目标只用于这一次恢复，不会写进连接列表。物理基准备份请下载 tar，不能灌回正在运行的库。</p>
                    <button class="primary">开始恢复</button>
                </form>
                <div class="list" style="margin-top:12px">${state.restores.map(restoreCard).join("") || ""}</div>
            </div>
        </div>`;
    },
    archive() {
        return `<div class="split">
            <form id="archive-form">
                <h2>按条件抽出一张表</h2>
                <label>数据库<select name="databaseId">${options(state.databases, "数据库")}</select></label>
                <label>存储<select name="storageId">${options(state.storages, "存储")}</select></label>
                <label>表<input name="table" required placeholder="orders"></label>
                <label>条件<input name="where" required value="create_time < '2025-01-01'"></label>
                <label>年份<input name="year" type="number" value="2024"></label>
                <label>每批删除<input name="batchSize" type="number" value="5000"></label>
                <label class="check"><input type="checkbox" name="deleteAfter"><span>MD5 一致后分批删除</span></label>
                <p class="muted">对象键是 dms_archives/库/表/年_archive.parquet。同一年再次归档会另存一份，通配查询仍能看见。</p>
                <button class="primary">开始归档</button>
            </form>
            <div class="list">${state.archives.map(archiveCard).join("") || `<p class="muted">还没有归档任务。</p>`}</div>
        </div>`;
    },
    cold() {
        return `<form id="cold-form">
            <h1>回头看已经搬走的行。</h1>
            <div class="split">
                <div>
                    <label>数据库<select name="databaseId">${options(state.databases, "数据库")}</select></label>
                    <label>存储<select name="storageId">${options(state.storages, "存储")}</select></label>
                    <label>表<input name="table" required></label>
                    <label>WHERE<input name="where" placeholder="id = 7"></label>
                    <button class="primary">查询，最多 100 行</button>
                </div>
                <div id="cold-result" class="card"><p>DuckDB 在进程里打开，S3 / 七牛走 httpfs 的区间读取。本地目录则直接读归档文件。</p></div>
            </div>
        </form>`;
    },
    notifiers() {
        const type = state.notifierType || "TELEGRAM";
        return `<div class="split">
            <form id="notifier-form">
                <h2>通知渠道</h2>
                <label>名称<input name="name" required></label>
                <label>类型<select name="type">${Object.keys(notifierFields).map((item) => `<option ${item === type ? "selected" : ""}>${item}</option>`).join("")}</select></label>
                ${notifierFields[type].map(([name, label, kind]) => field(name, label, kind)).join("")}
                <fieldset><legend>发送哪些事件</legend>${Object.entries(eventLabels).map(([id, label]) => `<label class="check"><input type="checkbox" name="event" value="${id}" ${defaultEvents.includes(id) ? "checked" : ""}><span>${label}</span></label>`).join("")}</fieldset>
                <fieldset><legend>只看这些数据库，都不勾选就是全部</legend>${state.databases.map((db) => `<label class="check"><input type="checkbox" name="databaseId" value="${esc(db.id)}"><span>${esc(db.name)}</span></label>`).join("") || `<p class="muted">还没有数据库。</p>`}</fieldset>
                <p class="muted">密码、Token 和 Webhook 地址加密后存进数据目录，页面上不再显示。</p>
                <button class="primary" type="submit">保存</button>
            </form>
            <div class="list">${state.notifiers.map(notifierCard).join("") || `<p class="muted">还没有通知渠道。</p>`}</div>
        </div>`;
    },
    audit() {
        if (!state.audit.length) return `<p class="muted">还没有记录。</p>`;
        return `<h1>谁在什么时候做了什么。</h1>
            <p class="muted">记录登录、口令变更和每一次会改动数据或配置的操作，保留最近 5000 条。不记录口令和存储密钥。</p>
            <div class="card table-wrap" style="margin-top:16px"><table><thead><tr><th>时间</th><th>操作</th><th>对象</th></tr></thead><tbody>${state.audit.map((row) => `<tr><td>${when(row.at)}</td><td>${esc(auditLabels[row.action] || row.action)}</td><td>${esc(row.detail)}</td></tr>`).join("")}</tbody></table></div>`;
    },
    ddl() {
        return `<div class="split">
            <form id="ddl-form">
                <h2>提交一条 ALTER</h2>
                <label>数据库<select name="databaseId">${options(state.databases, "数据库")}</select></label>
                <label>语句<textarea name="statement" required>ALTER TABLE orders ADD COLUMN note varchar(64)</textarea></label>
                <label>工具<select name="tool"><option value="gh-ost">gh-ost</option><option value="pt-osc">pt-online-schema-change</option></select></label>
                <label>Chunk<input name="chunkSize" type="number" value="1000"></label>
                <label class="check"><input type="checkbox" name="execute"><span>执行到切表。不勾选时 gh-ost 只演练</span></label>
                <button class="primary">启动</button>
            </form>
            <div>
                <article class="card">
                    <strong>影子表在工具里面</strong>
                    <p>空表 _表_gho、在空表上改结构、分批拷贝旧数据、把迁移期间的 binlog 追平，最后一次 RENAME 换名。dmsx 负责拉起进程并把速率推到这条进度条。</p>
                    <div class="bar" style="margin-top:14px"><span id="ddl-bar"></span></div>
                    <p id="ddl-meta" class="muted"></p>
                    <pre class="log" id="ddl-log"></pre>
                </article>
                <div class="list" style="margin-top:12px">${state.ddls.map(ddlCard).join("") || ""}</div>
            </div>
        </div>`;
    },
};

function dbCard(row) {
    const config = row.healthCheck || { enabled: true, notify: true, intervalMinutes: 1, failuresBeforeDown: 3 };
    const health = healthOf(row.id);
    const lastError = health.attempts.find((attempt) => attempt.status === "UNAVAILABLE");
    return `<article>
        <div class="row"><strong>${esc(row.name)}</strong><span class="tag">${esc(row.engine)}</span></div>
        <span class="muted">${esc(row.username)}@${esc(row.host)}:${row.port}/${esc(row.database)}</span>
        <div class="row"><span>${healthBadge(row)} <span class="muted">最近探测 ${when(health.checkedAt)}</span></span></div>
        ${health.status === "UNAVAILABLE" && lastError ? `<span class="err">${esc(lastError.error)}</span>` : ""}
        <div class="actions">
            <button class="ghost" data-test-db="${esc(row.id)}">测试连接</button>
            <button class="ghost" data-backup-user="${esc(row.id)}">建只读备份用户</button>
            ${row.engine === "postgresql" ? `<button class="ghost" data-replication-user="${esc(row.id)}">建复制用户</button>` : ""}
            <button class="danger" data-del-db="${esc(row.id)}">删除</button>
        </div>
        <details>
            <summary>健康检查</summary>
            <form class="inline" data-health-form="${esc(row.id)}">
                <label class="check"><input type="checkbox" name="enabled" ${config.enabled ? "checked" : ""}><span>定时探测连通性</span></label>
                <label>间隔分钟<input name="intervalMinutes" type="number" min="1" value="${config.intervalMinutes}"></label>
                <label>连续失败几次判为不可用<input name="failuresBeforeDown" type="number" min="1" value="${config.failuresBeforeDown}"></label>
                <label class="check"><input type="checkbox" name="notify" ${config.notify ? "checked" : ""}><span>状态变化时发通知</span></label>
                <button class="ghost" type="submit">保存</button>
            </form>
            <div class="attempts">${health.attempts.slice(0, 30).map((attempt) => `<span class="dot ${attempt.status === "AVAILABLE" ? "ok" : "bad"}" title="${esc(when(attempt.at))}${attempt.error ? " · " + esc(attempt.error) : ""}"></span>`).join("")}</div>
        </details>
    </article>`;
}
function notifierCard(row) {
    const scope = row.databaseIds.length ? row.databaseIds.map((id) => esc((state.databases.find((db) => db.id === id) || {}).name || id)).join("、") : "全部数据库";
    return `<article>
        <div class="row"><strong>${esc(row.name)}</strong><span class="tag">${esc(row.type)}</span></div>
        <span class="muted">${row.events.map((event) => esc(eventLabels[event] || event)).join("、")} · ${scope}</span>
        ${row.lastSentAt ? `<span class="muted">最近发送 ${when(row.lastSentAt)}${row.lastError ? "" : " · 成功"}</span>` : ""}
        ${row.lastError ? `<span class="err">${esc(row.lastError)}</span>` : ""}
        <div class="actions"><button class="ghost" data-test-notifier="${esc(row.id)}">发测试消息</button><button class="danger" data-del-notifier="${esc(row.id)}">删除</button></div>
    </article>`;
}
function storageCard(row) {
    return `<article><div class="row"><strong>${esc(row.name)}</strong><span class="tag">${esc(row.type)}</span></div><div class="actions"><button class="ghost" data-test-st="${esc(row.id)}">测试</button><button class="danger" data-del-st="${esc(row.id)}">删除</button></div></article>`;
}
function configCard(row) {
    const caps = [row.maxBackupSizeMb ? `单个 ≤ ${row.maxBackupSizeMb} MB` : "", row.maxTotalSizeMb ? `总量 ≤ ${row.maxTotalSizeMb} MB` : ""].filter(Boolean).join(" · ");
    return `<article><div class="row"><strong>${esc(row.name)}</strong><span class="tag">${esc(row.method)}</span></div><span class="muted">${esc(row.schedule.type)} · 保留 ${esc(row.retention.kind)}${caps ? " · " + caps : ""}</span><div class="actions"><button class="primary" data-run="${esc(row.id)}">立即备份</button><button class="danger" data-del-cfg="${esc(row.id)}">删除</button></div></article>`;
}
function runOptions(rows) {
    const ready = rows.filter((row) => row.status === "success");
    if (!ready.length) return `<option value="">还没有可恢复的备份</option>`;
    return ready.map((row) => `<option value="${esc(row.id)}">${esc(row.startedAt)} · ${esc(row.codec)}</option>`).join("");
}
function runCard(row) {
    return `<article><div class="row"><strong>${esc(row.status)}</strong><span class="muted">${esc(row.startedAt)}</span></div><span>${esc(row.fileName)} ${row.bytes ? `· ${size(row.bytes)}` : ""}</span>${row.error ? `<span class="err">${esc(row.error)}</span>` : ""}<div class="actions">${row.status === "running" ? `<button class="ghost" data-cancel="${esc(row.id)}">取消</button>` : ""}${row.status === "success" ? `<a href="/api/backups/runs/${esc(row.id)}/download">下载</a><button class="danger" data-del-run="${esc(row.id)}">删除</button>` : ""}</div></article>`;
}
function restoreCard(row) {
    return `<article><div class="row"><strong>${esc(row.database)}</strong><span class="tag">${esc(row.status)}</span></div><span class="muted">${esc(row.engine)}</span>${row.error ? `<span class="err">${esc(row.error)}</span>` : ""}${row.status === "running" ? `<button class="ghost" data-cancel="${esc(row.id)}">取消</button>` : ""}</article>`;
}
function archiveCard(row) {
    return `<article><div class="row"><strong>${esc(row.table)} · ${row.year}</strong><span class="tag">${esc(row.status)}</span></div><span class="muted">${esc(row.objectKey)}</span><span>归档 ${row.rowsArchived} 行，删除 ${row.rowsDeleted} 行${row.md5 ? ` · md5 ${esc(row.md5)}` : ""}</span>${row.error ? `<span class="err">${esc(row.error)}</span>` : ""}${row.status === "running" ? `<button class="ghost" data-cancel="${esc(row.id)}">取消</button>` : ""}</article>`;
}
function ddlCard(row) {
    return `<article><div class="row"><strong>${esc(row.table || "变更")}</strong><span class="tag">${esc(row.status)}</span></div><span class="muted">${esc(row.statement)}</span><button class="ghost" data-watch="${esc(row.id)}">查看进度</button></article>`;
}

function formData(form) {
    return Object.fromEntries(new FormData(form).entries());
}

function bind(view) {
    view.querySelectorAll("[data-test-db]").forEach((button) => button.onclick = () => act(() => api(`/api/databases/${button.dataset.testDb}/test`, { method: "POST" })));
    view.querySelectorAll("[data-del-db]").forEach((button) => button.onclick = () => act(async () => { await api(`/api/databases/${button.dataset.delDb}`, { method: "DELETE" }); await refresh(); draw(); }));
    view.querySelectorAll("[data-test-st]").forEach((button) => button.onclick = () => act(() => api(`/api/storages/${button.dataset.testSt}/test`, { method: "POST" })));
    view.querySelectorAll("[data-del-st]").forEach((button) => button.onclick = () => act(async () => { await api(`/api/storages/${button.dataset.delSt}`, { method: "DELETE" }); await refresh(); draw(); }));
    view.querySelectorAll("[data-run]").forEach((button) => button.onclick = () => act(async () => { await api(`/api/backups/configs/${button.dataset.run}/run`, { method: "POST" }); await refresh(); draw(); }));
    view.querySelectorAll("[data-del-cfg]").forEach((button) => button.onclick = () => act(async () => { await api(`/api/backups/configs/${button.dataset.delCfg}`, { method: "DELETE" }); await refresh(); draw(); }));
    view.querySelectorAll("[data-del-run]").forEach((button) => button.onclick = () => act(async () => { await api(`/api/backups/runs/${button.dataset.delRun}`, { method: "DELETE" }); await refresh(); draw(); }));
    view.querySelectorAll("[data-cancel]").forEach((button) => button.onclick = () => act(() => api(`/api/runs/${button.dataset.cancel}/cancel`, { method: "POST" })));
    view.querySelectorAll("[data-watch]").forEach((button) => button.onclick = () => watchDDL(button.dataset.watch));
    view.querySelectorAll("[data-backup-user], [data-replication-user]").forEach((button) => button.onclick = () => {
        const replication = button.hasAttribute("data-replication-user");
        const id = button.dataset.backupUser || button.dataset.replicationUser;
        const what = replication ? "带 REPLICATION 的复制用户（给物理备份用）" : "只能读、不能写的备份用户";
        if (!confirm(`用这条连接现在的账号，在服务器上新建一个${what}，然后把连接切换到新用户。新口令只存进 dmsx，不会显示。继续？`)) return;
        act(async () => {
            const row = await api(`/api/databases/${id}/backup-user`, { method: "POST", body: JSON.stringify({ replication }) });
            alert(`已切换到 ${row.username}`);
            await refresh(); draw();
        });
    });
    view.querySelectorAll("[data-health-form]").forEach((form) => form.onsubmit = (event) => {
        event.preventDefault();
        act(async () => {
            await api(`/api/databases/${form.dataset.healthForm}/healthcheck`, { method: "PUT", body: JSON.stringify({
                enabled: form.enabled.checked, notify: form.notify.checked,
                intervalMinutes: Number(form.intervalMinutes.value), failuresBeforeDown: Number(form.failuresBeforeDown.value),
            }) });
            await refresh(); draw();
        });
    });
    view.querySelectorAll("[data-test-notifier]").forEach((button) => button.onclick = () => act(async () => {
        try {
            await api(`/api/notifiers/${button.dataset.testNotifier}/test`, { method: "POST" });
            alert("已发送");
        } finally {
            await refresh(); draw();
        }
    }));
    view.querySelectorAll("[data-del-notifier]").forEach((button) => button.onclick = () => act(async () => { await api(`/api/notifiers/${button.dataset.delNotifier}`, { method: "DELETE" }); await refresh(); draw(); }));
    const notifierForm = view.querySelector("#notifier-form");
    if (notifierForm) {
        notifierForm.type.onchange = () => { state.notifierType = notifierForm.type.value; draw(); };
        notifierForm.onsubmit = async (event) => {
            event.preventDefault();
            const data = new FormData(notifierForm);
            const type = data.get("type");
            await act(async () => {
                await api("/api/notifiers", { method: "POST", body: JSON.stringify({
                    name: data.get("name"), type, settings: collect(notifierForm, notifierFields[type]),
                    events: data.getAll("event"), databaseIds: data.getAll("databaseId"),
                }) });
                await refresh(); draw();
            });
        };
    }

    const dbForm = view.querySelector("#db-form");
    if (dbForm) dbForm.onsubmit = async (event) => {
        event.preventDefault();
        const data = formData(dbForm);
        const body = {
            name: data.name, engine: data.engine, host: data.host, port: Number(data.port),
            username: data.username, password: data.password, database: data.database, version: data.version,
            ssl: dbForm.ssl.checked, includeTriggers: dbForm.includeTriggers.checked, includeEvents: dbForm.includeEvents.checked,
        };
        if (data.sshHost) body.ssh = { host: data.sshHost, port: Number(data.sshPort || 22), user: data.sshUser, password: data.sshPassword };
        await act(async () => { await api("/api/databases", { method: "POST", body: JSON.stringify(body) }); await refresh(); draw(); });
    };
    const storageForm = view.querySelector("#storage-form");
    if (storageForm) {
        storageForm.type.onchange = () => { state.storageType = storageForm.type.value; draw(); };
        storageForm.onsubmit = async (event) => {
            event.preventDefault();
            const data = formData(storageForm);
            const settings = {};
            Object.assign(settings, collect(storageForm, storageFields[data.type] || []));
            if (storageForm.tls?.checked) settings.tls = true;
            await act(async () => {
                await api("/api/storages", { method: "POST", body: JSON.stringify({ name: data.name, type: data.type, settings }) });
                await refresh(); draw();
            });
        };
    }
    const backupForm = view.querySelector("#backup-form");
    if (backupForm) backupForm.onsubmit = async (event) => {
        event.preventDefault();
        const data = formData(backupForm);
        const schedule = { type: data.scheduleType };
        if (data.scheduleType !== "HOURLY" && data.scheduleType !== "CRON") schedule.timeOfDay = data.timeOfDay;
        if (data.scheduleType === "WEEKLY") schedule.weekday = Number(data.count || 1);
        if (data.scheduleType === "MONTHLY") schedule.dayOfMonth = Number(data.count || 1);
        if (data.scheduleType === "CRON") schedule.cronExpression = data.timeOfDay;
        const retention = { kind: data.retention, period: "WEEK", count: Number(data.count || 7), gfs: { hours: 24, days: Number(data.count || 7), weeks: 4, months: 6, years: 1 } };
        const excludeTables = String(data.excludeTables || "").split(",").map((name) => name.trim()).filter(Boolean);
        await act(async () => {
            await api("/api/backups/configs", { method: "POST", body: JSON.stringify({
                name: data.name, databaseId: data.databaseId, storageId: data.storageId, method: data.method,
                schedule, retention, encrypted: backupForm.encrypted.checked, excludeTables, retry: true,
                maxBackupSizeMb: Number(data.maxBackupSizeMb || 0), maxTotalSizeMb: Number(data.maxTotalSizeMb || 0),
            }) });
            await refresh(); draw();
        });
    };
    const restoreForm = view.querySelector("#restore-form");
    if (restoreForm) restoreForm.onsubmit = async (event) => {
        event.preventDefault();
        const data = formData(restoreForm);
        await act(async () => {
            await api(`/api/backups/runs/${data.backupId}/restore`, { method: "POST", body: JSON.stringify({
                host: data.host, port: Number(data.port), username: data.username, password: data.password, database: data.database,
            }) });
            await refresh(); draw();
        });
    };
    const archiveForm = view.querySelector("#archive-form");
    if (archiveForm) archiveForm.onsubmit = async (event) => {
        event.preventDefault();
        const data = formData(archiveForm);
        await act(async () => {
            await api("/api/archive/jobs", { method: "POST", body: JSON.stringify({
                databaseId: data.databaseId, storageId: data.storageId, table: data.table, where: data.where,
                year: Number(data.year), batchSize: Number(data.batchSize), deleteAfter: archiveForm.deleteAfter.checked,
            }) });
            await refresh(); draw();
        });
    };
    const coldForm = view.querySelector("#cold-form");
    if (coldForm) coldForm.onsubmit = async (event) => {
        event.preventDefault();
        const data = formData(coldForm);
        try {
            const result = await api("/api/cold-query", { method: "POST", body: JSON.stringify({ databaseId: data.databaseId, storageId: data.storageId, table: data.table, where: data.where, limit: 100 }) });
            const rows = result.rows || [];
            const columns = rows[0] ? Object.keys(rows[0]) : [];
            $("cold-result").innerHTML = rows.length ? `<table><thead><tr>${columns.map((column) => `<th>${esc(column)}</th>`).join("")}</tr></thead><tbody>${rows.map((row) => `<tr>${columns.map((column) => `<td>${esc(row[column])}</td>`).join("")}</tr>`).join("")}</tbody></table>` : `<p>没有行。</p>`;
        } catch (error) {
            $("cold-result").innerHTML = `<p class="err">${esc(error.message)}</p>`;
        }
    };
    const ddlForm = view.querySelector("#ddl-form");
    if (ddlForm) ddlForm.onsubmit = async (event) => {
        event.preventDefault();
        const data = formData(ddlForm);
        await act(async () => {
            const run = await api("/api/ddl/runs", { method: "POST", body: JSON.stringify({
                databaseId: data.databaseId, statement: data.statement, tool: data.tool,
                chunkSize: Number(data.chunkSize), execute: ddlForm.execute.checked,
            }) });
            watchDDL(run.id);
            await refresh();
        });
    };
}

function watchDDL(id) {
    const log = $("ddl-log");
    const bar = $("ddl-bar");
    const meta = $("ddl-meta");
    if (!log) return;
    const socket = new WebSocket(`${location.origin.replace(/^http/, "ws")}/api/ddl/runs/${id}/stream`);
    socket.onmessage = (event) => {
        const message = JSON.parse(event.data);
        if (message.line) log.textContent += message.line + "\n";
        if (message.kind === "progress" && bar) {
            bar.style.width = `${message.progress.percent || 0}%`;
            meta.textContent = `${message.progress.copied || 0} / ${message.progress.total || "?"} · ETA ${message.progress.eta || "—"}`;
        }
        if (message.kind === "done") meta.textContent = message.status + (message.error ? " · " + message.error : "");
    };
}

async function act(fn) {
    try { await fn(); } catch (error) { alert(error.message); }
}

function showGate(session) {
    $("app").hidden = true;
    $("gate").hidden = false;
    const setup = !session.configured;
    $("gate-title").textContent = setup ? "设置口令" : "登录";
    $("gate-copy").textContent = setup ? "第一次使用。口令只保存在这台机器的数据目录里，之后用它进入。" : "输入口令进入管理台。";
    $("gate-submit").textContent = setup ? "设置并进入" : "进入";
    $("gate-form").dataset.mode = setup ? "setup" : "login";
    $("gate-error").textContent = "";
}

async function enter() {
    $("gate").hidden = true;
    $("app").hidden = false;
    $("health").textContent = "本机服务";
    $("health").classList.add("ok");
    await refresh();
    draw();
}

function applyTheme() {
    const theme = document.documentElement.dataset.theme;
    $("theme").setAttribute("aria-label", theme === "light" ? "切换到深色" : "切换到浅色");
    $("theme").innerHTML = theme === "light"
        ? `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z"/></svg>`
        : `<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M5.6 18.4L7 17M17 7l1.4-1.4"/></svg>`;
}

function applySidebar() {
    const collapsed = localStorage.getItem("dmsx.sidebar") === "collapsed";
    $("app").classList.toggle("collapsed", collapsed);
    $("collapse").setAttribute("aria-expanded", String(!collapsed));
    $("collapse").setAttribute("aria-label", collapsed ? "展开侧边栏" : "收起侧边栏");
}

async function boot() {
    applySidebar();
    applyTheme();
    $("theme").onclick = () => {
        const next = document.documentElement.dataset.theme === "light" ? "dark" : "light";
        document.documentElement.dataset.theme = next;
        localStorage.setItem("dmsx.theme", next);
        applyTheme();
    };
    $("collapse").onclick = () => {
        const collapsed = !$("app").classList.contains("collapsed");
        localStorage.setItem("dmsx.sidebar", collapsed ? "collapsed" : "open");
        applySidebar();
    };
    $("logout").onclick = async () => {
        await api("/api/session", { method: "DELETE" });
        showGate({ configured: true });
    };
    $("gate-form").onsubmit = async (event) => {
        event.preventDefault();
        const password = new FormData(event.target).get("password");
        const mode = event.target.dataset.mode;
        try {
            if (mode === "setup") await api("/api/setup", { method: "POST", body: JSON.stringify({ password }) });
            await api("/api/session", { method: "POST", body: JSON.stringify({ password }) });
            event.target.reset();
            await enter();
        } catch (error) {
            $("gate-error").textContent = error.message;
        }
    };
    try {
        const session = await api("/api/session");
        if (!session.authenticated) showGate(session);
        else await enter();
    } catch (error) {
        $("health").textContent = "服务未就绪";
        showGate({ configured: true });
    }
    setInterval(async () => {
        if ($("app").hidden || !["overview", "databases", "backups", "archive", "ddl", "notifiers"].includes(state.section)) return;
        if (document.activeElement && document.activeElement.closest("form")) return;
        if (document.querySelector("#view details[open]")) return;
        try {
            await refresh();
            draw();
        } catch (error) {
            if (error.status === 401) showGate({ configured: true });
        }
    }, 4000);
}

boot();
