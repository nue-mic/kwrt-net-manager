import { test, expect } from './fixtures/daemon';
import { login } from './helpers/selectors';

test.describe('登录流程', () => {
  test('正确 token 可登录到主界面', async ({ page, daemon }) => {
    await page.goto(daemon.baseURL);
    await expect(login.tokenInput(page)).toBeVisible();

    await login.tokenInput(page).fill(daemon.token);
    await login.submitBtn(page).click();

    await expect(page).not.toHaveURL(/login/);
    await expect(
      page.getByRole('menuitem', { name: /FRPC 实例|实例|仪表盘|dashboard/i }).first(),
    ).toBeVisible();
  });

  test('错误 token 应停在登录页', async ({ page, daemon }) => {
    await page.goto(daemon.baseURL);
    await login.tokenInput(page).fill('wrong-token-xxx');
    await login.submitBtn(page).click();
    // 应该仍然能看到登录输入框（没跳走）
    await expect(login.tokenInput(page)).toBeVisible();
  });
});
