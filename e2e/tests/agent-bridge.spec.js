const { test, expect } = require('@playwright/test');

test.describe('AgentBridge GUI (Port 9095)', () => {

    test('Should load inbox and display connected agent', async ({ page }) => {
        await page.goto('http://localhost:9095');
        
        // Check Agent Name
        const name = page.locator('#my-name');
        await expect(name).toHaveText('Antigravity', { timeout: 10000 });
        
        // Ensure inbox list loads
        const inbox = page.locator('#inbox-list');
        await expect(inbox).toBeVisible();
    });

    test('Should have datalist for recipient autofill', async ({ page }) => {
        await page.goto('http://localhost:9095');
        
        const recipientInput = page.locator('#recipient');
        await expect(recipientInput).toHaveAttribute('list', 'known-agents');
        
        const dataList = page.locator('#known-agents');
        await expect(dataList).toBeAttached();
    });

    test('Cross-link to Worker Node works', async ({ page }) => {
        await page.goto('http://localhost:9095');
        
        const workerBtn = page.getByRole('link', { name: 'Worker Node' });
        await expect(workerBtn).toBeVisible();
        await expect(workerBtn).toHaveAttribute('href', 'http://localhost:8081');
    });

});
