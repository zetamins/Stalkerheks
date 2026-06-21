package com.stalkerhek.app;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.widget.Toast;

/**
 * Starts stalkerhek server in background when device boots.
 * Shows a toast so the user knows the server is running.
 */
public class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        if (Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) {
            Toast.makeText(context, "Stalkerhek starting in background...", Toast.LENGTH_LONG).show();
            String dbDir = context.getFilesDir().getAbsolutePath();
            new Thread(() -> {
                try {
                    String binPath = dbDir + "/stalkerhek";
                    String assetName = detectArch();
                    // Extract binary
                    java.io.InputStream in = context.getAssets().open(assetName);
                    java.io.FileOutputStream out = new java.io.FileOutputStream(binPath);
                    byte[] buf = new byte[8192];
                    int n;
                    while ((n = in.read(buf)) > -1) out.write(buf, 0, n);
                    in.close(); out.close();
                    new java.io.File(binPath).setExecutable(true);

                    // Start server silently
                    Runtime.getRuntime().exec(
                        new String[]{binPath, "-profile", "default", "-db", dbDir});
                } catch (Exception e) {
                    android.util.Log.e("Stalkerhek", "Boot start failed", e);
                }
            }).start();
        }
    }

    private String detectArch() {
        String abi = android.os.Build.SUPPORTED_ABIS[0];
        if (abi.contains("x86_64")) return "stalkerhek-x86_64";
        else if (abi.contains("arm64")) return "stalkerhek-arm64";
        else return "stalkerhek-arm";
    }
}
