import { test, expect, type Page } from './fixtures/daemon';
import { login } from './helpers/selectors';

/**
 * 每页条数切换 —— 回归测试。
 *
 * 曾经的 bug：pagination 里传的是受控的 `pageSize: 20`，用户在下拉里选了别的档位，
 * 组件内部 state 改了，下一次 render 又被 props 的 20 覆盖回去 —— 选项列得出来，
 * 选了却没反应。修法是改用非受控的 `defaultPageSize`。
 */

/** 分页器里「N 条/页」那个下拉所在的 li（避免与下拉浮层里的同名选项冲突）。 */
const sizeBox = (p: Page) => p.locator('li').filter({ has: p.getByRole('combobox', { name: '页码' }) });
const rows = (p: Page) => p.locator('.ant-table-tbody tr.ant-table-row');

async function pickPageSize(p: Page, label: string) {
  await sizeBox(p).getByRole('combobox', { name: '页码' }).click();
  await p.getByRole('option', { name: label, exact: true }).click();
}

/**
 * 造 count 条静态分配（→ 终端列表里也会各出现一条静态租约），让数据量超过一页。
 * 注意：选中值的文字是 Select 自己的 state，受控 bug 下也会跟着变；真正能证明
 * 切换生效的是表格行数，所以用例必须断言行数。
 */
async function seedStatics(baseURL: string, token: string, startHost: number, count: number) {
  const h = { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' };
  for (let i = 0; i < count; i++) {
    const n = startHost + i;
    const r = await fetch(`${baseURL}/api/v1/dhcp/statics`, {
      method: 'POST',
      headers: h,
      body: JSON.stringify({
        hostname: `e2e-${n}`,
        ip: `192.168.1.${n}`,
        mac: `AA:BB:CC:00:01:${n.toString(16).padStart(2, '0').toUpperCase()}`,
        gateway: '',
        interface: 'lan',
        dns_primary: '',
        dns_secondary: '',
        remark: 'e2e',
        route_push: false,
        enabled: true,
      }),
    });
    if (!r.ok) throw new Error(`创建静态分配失败: ${r.status} ${await r.text()}`);
  }
}

test.describe('列表每页条数', () => {
  test.beforeEach(async ({ page, daemon }) => {
    await page.goto(daemon.baseURL);
    await login.tokenInput(page).fill(daemon.token);
    await login.submitBtn(page).click();
    await expect(page).not.toHaveURL(/login/);
  });

  test('DHCP 终端列表：切换每页条数应生效且不回弹', async ({ page, daemon }) => {
    // 造够数据（seed 只有 7 条），否则「每页 10 条」看不出分页差别。
    await seedStatics(daemon.baseURL, daemon.token, 60, 25);

    await page.goto(`${daemon.baseURL}/dhcp/leases`);
    await expect(rows(page).first()).toBeVisible();
    await expect(sizeBox(page)).toContainText('20 条/页');

    const total = Number((await page.locator('.ant-pagination-total-text').innerText()).replace(/\D/g, ''));
    expect(total).toBeGreaterThan(20); // 前置条件：数据足够多，分页才有意义
    await expect(rows(page)).toHaveCount(20);

    // 新增的大档位必须都在选项里
    await sizeBox(page).getByRole('combobox', { name: '页码' }).click();
    for (const label of ['200 条/页', '500 条/页', '1000 条/页', '2000 条/页']) {
      await expect(page.getByRole('option', { name: label, exact: true })).toBeVisible();
    }
    await page.keyboard.press('Escape');

    // 切到 10 条/页：选中值留在 10，且真的只剩 10 行
    await pickPageSize(page, '10 条/页');
    await expect(sizeBox(page)).toContainText('10 条/页');
    await expect(rows(page)).toHaveCount(10);

    // 切到 1000 条/页：全部数据落在一页
    await pickPageSize(page, '1000 条/页');
    await expect(sizeBox(page)).toContainText('1000 条/页');
    await expect(rows(page)).toHaveCount(total);
  });

  test('DHCP 静态分配：切换每页条数应生效', async ({ page, daemon }) => {
    await seedStatics(daemon.baseURL, daemon.token, 140, 15);

    await page.goto(`${daemon.baseURL}/dhcp/statics`);
    await expect(rows(page).first()).toBeVisible();

    await pickPageSize(page, '10 条/页');
    await expect(sizeBox(page)).toContainText('10 条/页');
    await expect(rows(page)).toHaveCount(10);
  });
});
