package com.stalkerhek.app;

import android.app.Activity;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.Gravity;
import android.view.View;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.webkit.WebSettings;
import android.widget.FrameLayout;
import android.widget.ProgressBar;
import android.widget.TextView;
import android.widget.LinearLayout;

/**
 * Stalkerhek Android App — runs the stalkerhek proxy server and displays
 * the dashboard in a full-screen WebView.
 *
 * Startup flow:
 *   1. Show splash: dark screen with spinner + "Starting stalkerhek..."
 *   2. Go server starts in background (proxy + HLS + dashboard)
 *   3. WebView polls dashboard until it's ready, then fades in
 */
public class MainActivity extends Activity {

    private WebView webView;
    private LinearLayout splashView;
    private Handler handler = new Handler(Looper.getMainLooper());
    private int pollAttempts = 0;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        FrameLayout root = new FrameLayout(this);

        // ---- SPLASH SCREEN ----
        splashView = new LinearLayout(this);
        splashView.setOrientation(LinearLayout.VERTICAL);
        splashView.setGravity(Gravity.CENTER);
        splashView.setBackgroundColor(Color.parseColor("#0a0f0a"));

        ProgressBar spinner = new ProgressBar(this);
        spinner.getIndeterminateDrawable().setTint(Color.parseColor("#2d7a4e"));
        spinner.setScaleX(1.3f);
        spinner.setScaleY(1.3f);

        TextView title = new TextView(this);
        title.setText("Stalkerhek");
        title.setTextColor(Color.parseColor("#e0e6e0"));
        title.setTextSize(24);
        title.setPadding(0, 30, 0, 8);

        TextView subtitle = new TextView(this);
        subtitle.setText("Starting services...");
        subtitle.setTextColor(Color.parseColor("#9aaa9a"));
        subtitle.setTextSize(14);

        splashView.addView(spinner);
        splashView.addView(title);
        splashView.addView(subtitle);
        root.addView(splashView);

        // ---- WEBVIEW (hidden initially) ----
        webView = new WebView(this);
        webView.setVisibility(View.GONE);
        webView.setWebViewClient(new WebViewClient() {
            @Override
            public void onPageFinished(WebView view, String url) {
                // Dashboard loaded — show it
                webView.setAlpha(0f);
                webView.setVisibility(View.VISIBLE);
                webView.animate().alpha(1f).setDuration(400).start();
                splashView.animate().alpha(0f).setDuration(300)
                    .withEndAction(() -> splashView.setVisibility(View.GONE)).start();
            }
        });
        WebSettings settings = webView.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setCacheMode(WebSettings.LOAD_DEFAULT);
        root.addView(webView);

        setContentView(root);

        // Extract native Go binary from assets and start in background.
        // Auto-detect architecture: ARM64 for phones, ARM32 for older TVs.
        final String dbDir = getFilesDir().getAbsolutePath();
        final String binPath = dbDir + "/stalkerhek";
        final String assetName = android.os.Build.SUPPORTED_64_BIT_ABIS.length > 0
            ? "stalkerhek-arm64" : "stalkerhek-arm";
        new Thread(() -> {
            try {
                // Copy binary from assets to internal storage
                java.io.InputStream in = getAssets().open(assetName);
                java.io.FileOutputStream out = new java.io.FileOutputStream(binPath);
                byte[] buf = new byte[8192];
                int n;
                while ((n = in.read(buf)) > -1) out.write(buf, 0, n);
                in.close(); out.close();
                // Make executable
                new java.io.File(binPath).setExecutable(true);

                // Start stalkerhek server
                java.lang.Process proc = Runtime.getRuntime().exec(
                    new String[]{binPath, "-profile", "default", "-db", dbDir});
                // Log output for debugging
                java.io.BufferedReader reader = new java.io.BufferedReader(
                    new java.io.InputStreamReader(proc.getInputStream()));
                String line;
                while ((line = reader.readLine()) != null) {
                    android.util.Log.i("Stalkerhek", line);
                }
            } catch (Exception e) {
                android.util.Log.e("Stalkerhek", "Server start failed", e);
            }
        }).start();

        // Poll until dashboard is ready, then load it
        pollDashboard();
    }

    private void pollDashboard() {
        new Thread(() -> {
            try {
                java.net.URL url = new java.net.URL("http://127.0.0.1:8080/");
                java.net.HttpURLConnection conn = (java.net.HttpURLConnection) url.openConnection();
                conn.setConnectTimeout(2000);
                conn.setReadTimeout(2000);
                int code = conn.getResponseCode();
                conn.disconnect();
                if (code == 200) {
                    handler.post(() -> webView.loadUrl("http://127.0.0.1:8080/"));
                    return;
                }
            } catch (Exception e) {
                // Server not ready yet
            }
            pollAttempts++;
            if (pollAttempts < 30) {
                handler.postDelayed(this::pollDashboard, 1000);
            } else {
                // Timeout — load anyway, dashboard might start late
                handler.post(() -> webView.loadUrl("http://127.0.0.1:8080/"));
            }
        }).start();
    }

    @Override
    public void onBackPressed() {
        if (webView.canGoBack() && webView.getVisibility() == View.VISIBLE) {
            webView.goBack();
        } else {
            moveTaskToBack(true); // minimize, keep server running
        }
    }

    @Override
    protected void onDestroy() {
        if (webView != null) {
            webView.destroy();
        }
        super.onDestroy();
    }
}
