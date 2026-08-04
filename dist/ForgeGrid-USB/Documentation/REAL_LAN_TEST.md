# Physical LAN Test Instructions

To verify the robust, secure LAN communication between the Coordinator and a physical Windows worker, follow these exact steps.

## Coordinator (Fedora HP Mini)
1. Ensure the port is open in the firewall:
   `sudo firewall-cmd --add-port=8080/tcp --permanent`
   `sudo firewall-cmd --reload`
2. Start the Coordinator:
   `./dist/ForgeGrid-USB/Linux/forgegrid -mode coordinator -port 8080`
3. Record the output on the terminal. It should state:
   - LAN IP (e.g., `10.245.173.178`)
   - TLS Fingerprint (e.g., `a1b2c3d4...`)
4. In the browser dashboard, click "Pair New Worker" to generate a code.

## Worker (Windows Laptop)
1. Copy the `dist/ForgeGrid-USB/Windows` directory to the laptop.
2. Open PowerShell or Command Prompt.
3. Run the worker with the IP, the Code, and the TLS Fingerprint you recorded from the Coordinator:
   `.\ForgeGrid.exe -mode worker -name "Windows-Worker-1" -coordinator 10.245.173.178 -code <CODE> -fingerprint <TLS_FINGERPRINT>`
4. The worker should successfully pair and begin sending heartbeats.

## Validation
1. **API Output**: On the Coordinator machine, run:
   `curl -k -s https://127.0.0.1:8080/api/workers`
   Verify it returns the hardware information (OS, RAM, CPU) WITHOUT any `token` or `token_hash` field.
2. **Dashboard Screenshot**: The Dashboard should show the worker as `online` with correct hardware specs.
3. **Run Test Job**: Click "Run Test Job" on the Dashboard. Verify the worker terminal logs the challenge received and the SHA-256 result computed, and the Dashboard updates to `completed`.
4. **Reconnection (Credential Persistence)**: 
   - Close the Windows worker terminal. Wait 20 seconds. The Dashboard should show the worker as `offline`. 
   - Restart the worker **without** the `-code`, `-fingerprint`, or `-name` flags:
     `.\ForgeGrid.exe -mode worker`
   - It should immediately state `Loaded saved worker credentials` and resume heartbeats, returning to `online` on the Dashboard.
5. **Resetting Credentials**:
   - Stop the worker and run:
     `.\ForgeGrid.exe -mode worker --reset-worker`
   - It should output `Worker credentials reset successfully`. Starting `.\ForgeGrid.exe -mode worker` should now throw an error demanding a code.
