package com.stalkerhek.app;

import android.app.Activity;
import android.content.Intent;
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
import android.widget.Button;
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

        // Show privacy disclosure on first launch (Play Protect compliance).
        // This app runs a LOCAL IPTV proxy — no data leaves your device.
        if (!getSharedPreferences("stalkerhek", MODE_PRIVATE).getBoolean("agreed", false)) {
            showPrivacyDialog();
            return;
        }

        startServer();
    }

    private void showPrivacyDialog() {
        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        layout.setGravity(Gravity.CENTER);
        layout.setBackgroundColor(Color.parseColor("#0a0f0a"));
        layout.setPadding(40, 40, 40, 40);

        TextView icon = new TextView(this);
        icon.setText("📡");
        icon.setTextSize(48);
        icon.setGravity(Gravity.CENTER);
        layout.addView(icon);

        TextView title = new TextView(this);
        title.setText("Stalkerhek");
        title.setTextColor(Color.parseColor("#e0e6e0"));
        title.setTextSize(22);
        title.setGravity(Gravity.CENTER);
        title.setPadding(0, 16, 0, 12);
        layout.addView(title);

        TextView body = new TextView(this);
        body.setText("This app runs a local IPTV proxy server on your device.\n\n" +
            "• No data is collected or sent anywhere\n" +
            "• All traffic stays on your local network\n" +
            "• Only network access is used (no SMS, files, contacts)\n" +
            "• Open-source — github.com/zetamins/Stalkerheks\n\n" +
            "This is a legitimate server tool, not malware.\n" +
            "If Play Protect warned you, tap 'More details'\n" +
            "and choose 'Install anyway'.");
        body.setTextColor(Color.parseColor("#9aaa9a"));
        body.setTextSize(14);
        body.setGravity(Gravity.CENTER);
        layout.addView(body);

        android.widget.Button agree = new android.widget.Button(this);
        agree.setText("I Understand — Start Server");
        agree.setBackgroundColor(Color.parseColor("#2d7a4e"));
        agree.setTextColor(Color.WHITE);
        agree.setPadding(32, 16, 32, 16);
        agree.setOnClickListener(v -> {
            getSharedPreferences("stalkerhek", MODE_PRIVATE).edit().putBoolean("agreed", true).apply();
            startServer();
        });
        layout.addView(agree);

        setContentView(layout);
    }

    private void startServer() {
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


		// ---- BOTTOM BAR (restart + kill) ----
		LinearLayout bottomBar = new LinearLayout(this);
		bottomBar.setOrientation(LinearLayout.HORIZONTAL);
		bottomBar.setGravity(Gravity.CENTER);
		bottomBar.setBackgroundColor(Color.parseColor("#0d1410"));
		bottomBar.setPadding(8, 10, 8, 10);

		Button restartBtn = new Button(this);
		restartBtn.setText("Restart");
		restartBtn.setTextColor(Color.WHITE);
		restartBtn.setBackgroundColor(Color.parseColor("#2d7a4e"));
		restartBtn.setOnClickListener(v -> {
			Intent intent = getBaseContext().getPackageManager()
				.getLaunchIntentForPackage(getBaseContext().getPackageName());
			intent.addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_NEW_TASK);
			startActivity(intent);
			android.os.Process.killProcess(android.os.Process.myPid());
			System.exit(0);
		});

		Button killBtn = new Button(this);
		killBtn.setText("Kill");
		killBtn.setTextColor(Color.WHITE);
		killBtn.setBackgroundColor(Color.parseColor("#e85d4d"));
		killBtn.setOnClickListener(v -> {
			android.os.Process.killProcess(android.os.Process.myPid());
			System.exit(0);
		});

		bottomBar.addView(restartBtn,
			new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));
		bottomBar.addView(killBtn,
			new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

		FrameLayout.LayoutParams barParams = new FrameLayout.LayoutParams(
			FrameLayout.LayoutParams.MATCH_PARENT,
			FrameLayout.LayoutParams.WRAP_CONTENT);
		barParams.gravity = Gravity.BOTTOM;
		root.addView(bottomBar, barParams);

        setContentView(root);

        // Extract native Go binary from assets, auto-detect architecture.
        // ARM64 (phones), ARM32 (older TVs), x86_64 (emulators).
        final String dbDir = getFilesDir().getAbsolutePath();
        final String binPath = dbDir + "/stalkerhek";
        final String assetName;
        String abi = android.os.Build.SUPPORTED_ABIS[0];
        if (abi.contains("x86_64")) assetName = "stalkerhek-x86_64";
        else if (abi.contains("arm64")) assetName = "stalkerhek-arm64";
        else assetName = "stalkerhek-arm64";
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
