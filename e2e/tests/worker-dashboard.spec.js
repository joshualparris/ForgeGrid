const { test, expect } = require('@playwright/test');

test.describe('Worker Dashboard (Port 8081)', () => {
    
    test('Should load and display worker identity correctly', async ({ page }) => {
        await page.goto('http://localhost:8081');
        
        // Wait for connection to go Online
        const status = page.locator('#connection-status');
        await expect(status).toHaveText('Online', { timeout: 10000 });
        
        // Check Node name
        const nodeName = page.locator('#node-name');
        await expect(nodeName).toHaveText('ThinkPad-Lenovo');
    });

    test('Should display hardware telemetry', async ({ page }) => {
        await page.goto('http://localhost:8081');
        
        // Ensure RAM usage updates (not 0%)
        const ramUsage = page.locator('#ram-usage');
        await expect(ramUsage).not.toHaveText('0%', { timeout: 10000 });
        
        // Check CPU cores
        const cpuCores = page.locator('#cpu-cores');
        await expect(cpuCores).not.toHaveText('0');
    });

    test('Cross-link to Messages works', async ({ page }) => {
        await page.goto('http://localhost:8081');
        
        const messagesBtn = page.getByRole('link', { name: 'Messages' });
        await expect(messagesBtn).toBeVisible();
        await expect(messagesBtn).toHaveAttribute('href', 'http://localhost:9095');
    });

});
