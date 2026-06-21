package com.stalkerhek.app;

import android.app.Activity;
import android.os.Bundle;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.webkit.WebSettings;

/**
 * Stalkerhek Android App — runs the stalkerhek proxy server and displays
 * the dashboard in a full-screen WebView. The Go server is started via
 * gomobile bindings (mobile.StartServer).
 *
 * Build prerequisites:
 *   1. gomobile init
 *   2. gomobile bind -target=android -androidapi 21 -o app.aar ./mobile
 *   3. Place app.aar in android/app/libs/
 *   4. Build with Android Studio or ./gradlew assembleRelease
 */
public class MainActivity extends Activity {

    private WebView webView;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        // Start stalkerhek Go server in a background thread.
        // The Go library handles proxy + HLS + dashboard startup.
        final String dbDir = getFilesDir().getAbsolutePath();
        final String profileName = "default";

        new Thread(() -> {
            try {
                // mobile.StartServer starts all services and returns the dashboard port.
                // This call blocks while services run.
                mobile.Mobile.startServer(dbDir, profileName);
            } catch (Exception e) {
                android.util.Log.e("Stalkerhek", "Server start failed", e);
            }
        }).start();

        // Create full-screen WebView for the dashboard
        webView = new WebView(this);
        webView.setWebViewClient(new WebViewClient());
        WebSettings settings = webView.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setAllowFileAccess(false);
        settings.setCacheMode(WebSettings.LOAD_DEFAULT);

        // Load the dashboard after a short delay to let the server start
        webView.postDelayed(() -> {
            webView.loadUrl("http://127.0.0.1:8080/");
        }, 3000);

        setContentView(webView);
    }

    @Override
    public void onBackPressed() {
        if (webView.canGoBack()) {
            webView.goBack();
        } else {
            // Minimize rather than exit (keeps server running in background)
            moveTaskToBack(true);
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
