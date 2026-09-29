const capability = document.querySelector('meta[name="teamcross-desktop"]').content;
const connection = document.querySelector('#connection');
const result = document.querySelector('#result');
const save = document.querySelector('#save');
async function request(write = false) {
  if (write) save.disabled = true;
  try {
    const response = await fetch('/api/ui-language', {
      method: write ? 'POST' : 'GET',
      headers: {'X-TeamCross-Desktop': capability, 'Content-Type': 'application/json'},
      ...(write ? {body: JSON.stringify({mode: 'zh-Hans'})} : {}),
    });
    const value = await response.json();
    connection.textContent = response.ok ? '已连接 / Connected' : '暂时无法连接 / Unavailable';
    result.textContent = response.ok ? JSON.stringify(value, null, 2) : '请恢复测试服务后重新读取；写入不会自动重试。 / Restore the fixture, then refresh. Writes are never replayed.';
  } catch {
    connection.textContent = '暂时无法连接 / Unavailable';
    result.textContent = '请重新读取以确认当前状态。 / Refresh to check the current state.';
  } finally { save.disabled = false; }
}
document.querySelector('#refresh').addEventListener('click', () => request());
save.addEventListener('click', () => request(true));
void request();
