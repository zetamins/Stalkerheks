package com.stalkerhek.app;

import android.app.Activity;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.graphics.drawable.GradientDrawable;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.method.LinkMovementMethod;
import android.view.Gravity;
import android.view.View;
import android.widget.Button;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.TextView;
import android.widget.Toast;

import com.google.zxing.BarcodeFormat;
import com.google.zxing.WriterException;
import com.google.zxing.common.BitMatrix;
import com.google.zxing.qrcode.QRCodeWriter;

import java.net.Inet4Address;
import java.net.NetworkInterface;

/**
 * Stalkerhek Android App — native UI with QR code, connection URLs,
 * and server controls. The dashboard is opened externally via clickable link.
 */
public class MainActivity extends Activity {

    private Handler handler = new Handler(Looper.getMainLooper());
    private String proxyPort = "8888";
    private String hlsPort = "9999";
    private String dashPort = "8080";
    private String host = "127.0.0.1";

    private boolean serverReady = false;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        if (!getSharedPreferences("stalkerhek", MODE_PRIVATE).getBoolean("agreed", false)) {
            showPrivacyDialog();
            return;
        }

        // Show loading screen while server starts
        showLoadingScreen();

        // Start server as foreground service
        Intent si = new Intent(this, ServerService.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(si);
        } else {
            startService(si);
        }

        // Poll until server is ready, then show main UI
        pollServer();
    }

    private void showLoadingScreen() {
        LinearLayout loading = new LinearLayout(this);
        loading.setOrientation(LinearLayout.VERTICAL);
        loading.setGravity(Gravity.CENTER);
        loading.setBackgroundColor(Color.parseColor("#0a0f0a"));

        ProgressBar spinner = new ProgressBar(this);
        spinner.getIndeterminateDrawable().setTint(Color.parseColor("#2d7a4e"));
        loading.addView(spinner);

        TextView txt = new TextView(this);
        txt.setText("Starting Stalkerhek...");
        txt.setTextColor(Color.parseColor("#9aaa9a"));
        txt.setTextSize(16);
        txt.setPadding(0, 20, 0, 0);
        loading.addView(txt);

        setContentView(loading);
    }

    private void pollServer() {
        if (serverReady) return;
        new Thread(() -> {
            try {
                java.net.URL url = new java.net.URL("http://127.0.0.1:8080/");
                java.net.HttpURLConnection conn = (java.net.HttpURLConnection) url.openConnection();
                conn.setConnectTimeout(1500);
                int code = conn.getResponseCode();
                conn.disconnect();
                if (code == 200) {
                    handler.post(() -> {
                        serverReady = true;
                        buildUI();
                    });
                    return;
                }
            } catch (Exception e) {}
            handler.postDelayed(this::pollServer, 1500);
        }).start();
    }

    private void buildUI() {
        // Detect WiFi IP for LAN access
        try {
            for (NetworkInterface iface : java.util.Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!iface.isLoopback() && iface.isUp()) {
                    for (java.net.InetAddress addr : java.util.Collections.list(iface.getInetAddresses())) {
                        if (addr instanceof Inet4Address) {
                            host = addr.getHostAddress();
                            break;
                        }
                    }
                }
            }
        } catch (Exception e) {}

        String proxyURL = "http://" + host + ":" + proxyPort + "/c/";
        String hlsURL = "http://" + host + ":" + hlsPort + "/iptv/";
        String dashURL = "http://" + host + ":" + dashPort + "/";

        // ---- NATIVE UI LAYOUT ----
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setGravity(Gravity.CENTER_HORIZONTAL);
        root.setBackgroundColor(Color.parseColor("#0a0f0a"));
        root.setPadding(40, 60, 40, 40);

        // Status dot + logo
        LinearLayout header = new LinearLayout(this);
        header.setGravity(Gravity.CENTER);
        header.setOrientation(LinearLayout.HORIZONTAL);
        View dot = new View(this);
        GradientDrawable dotBg = new GradientDrawable();
        dotBg.setShape(GradientDrawable.OVAL);
        dotBg.setColor(0xFF3fb970);
        dot.setBackground(dotBg);
        dot.setLayoutParams(new LinearLayout.LayoutParams(12, 12));
        dot.setBackgroundColor(Color.parseColor("#3fb970"));
        TextView logo = new TextView(this);
        logo.setText("  Stalkerhek");
        logo.setTextColor(Color.parseColor("#e0e6e0"));
        logo.setTextSize(22);
        header.addView(dot);
        header.addView(logo);
        root.addView(header);

        // Active profile
        TextView profile = new TextView(this);
        profile.setText("Profile: default");
        profile.setTextColor(Color.parseColor("#9aaa9a"));
        profile.setTextSize(13);
        profile.setPadding(0, 8, 0, 20);
        root.addView(profile);

        // QR Code
        try {
            Bitmap qr = generateQR(dashURL, 240);
            ImageView qrView = new ImageView(this);
            qrView.setImageBitmap(qr);
            qrView.setPadding(0, 0, 0, 8);
            LinearLayout.LayoutParams qrParams = new LinearLayout.LayoutParams(240, 240);
            qrView.setLayoutParams(qrParams);
            root.addView(qrView);
        } catch (Exception e) {
            TextView qrErr = new TextView(this);
            qrErr.setText("QR unavailable");
            qrErr.setTextColor(Color.parseColor("#e85d4d"));
            root.addView(qrErr);
        }

        TextView qrLabel = new TextView(this);
        qrLabel.setText("Scan to open dashboard");
        qrLabel.setTextColor(Color.parseColor("#6b7280"));
        qrLabel.setTextSize(12);
        qrLabel.setPadding(0, 4, 0, 24);
        root.addView(qrLabel);

        // URL cards
        root.addView(urlCard("Dashboard", dashURL, true));
        root.addView(urlCard("STB Proxy", proxyURL, true));
        root.addView(urlCard("HLS Streams", hlsURL + "<channel>", true));

        // Spacer
        View spacer = new View(this);
        spacer.setLayoutParams(new LinearLayout.LayoutParams(1, 20));
        root.addView(spacer);

        // Restart button
        Button restartBtn = new Button(this);
        restartBtn.setText("Restart Server");
        restartBtn.setTextColor(Color.WHITE);
        restartBtn.setBackgroundColor(Color.parseColor("#2d7a4e"));
        restartBtn.setPadding(24, 14, 24, 14);
        restartBtn.setOnClickListener(v -> {
            // Stop and restart the foreground service
            Intent si = new Intent(this, ServerService.class);
            stopService(si);
            handler.postDelayed(() -> {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    startForegroundService(new Intent(this, ServerService.class));
                } else {
                    startService(new Intent(this, ServerService.class));
                }
                Toast.makeText(this, "Server restarted", Toast.LENGTH_SHORT).show();
            }, 1000);
        });
        root.addView(restartBtn);

        // Kill button
        Button killBtn = new Button(this);
        killBtn.setText("Kill Server");
        killBtn.setTextColor(Color.WHITE);
        killBtn.setBackgroundColor(Color.parseColor("#e85d4d"));
        killBtn.setPadding(24, 12, 24, 12);
        killBtn.setOnClickListener(v -> {
            stopService(new Intent(this, ServerService.class));
            Toast.makeText(this, "Server stopped", Toast.LENGTH_SHORT).show();
            finish();
        });
        root.addView(killBtn);

        // ---- SCROLLABLE WRAPPER ----
        android.widget.ScrollView scroll = new android.widget.ScrollView(this);
        scroll.addView(root);
        scroll.setBackgroundColor(Color.parseColor("#0a0f0a"));
        setContentView(scroll);
    }

    private LinearLayout urlCard(String label, String url, boolean copyable) {
        LinearLayout card = new LinearLayout(this);
        card.setOrientation(LinearLayout.HORIZONTAL);
        card.setGravity(Gravity.CENTER_VERTICAL);
        card.setBackgroundColor(Color.parseColor("#0d1410"));
        card.setPadding(14, 12, 14, 12);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        params.setMargins(0, 0, 0, 8);
        card.setLayoutParams(params);

        LinearLayout textCol = new LinearLayout(this);
        textCol.setOrientation(LinearLayout.VERTICAL);
        textCol.setLayoutParams(new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        TextView lbl = new TextView(this);
        lbl.setText(label);
        lbl.setTextColor(Color.parseColor("#9aaa9a"));
        lbl.setTextSize(11);
        textCol.addView(lbl);

        TextView val = new TextView(this);
        val.setText(url);
        val.setTextColor(Color.parseColor("#3fb970"));
        val.setTextSize(13);
        val.setMovementMethod(LinkMovementMethod.getInstance());
        textCol.addView(val);

        card.addView(textCol);

        if (copyable) {
            Button copyBtn = new Button(this);
            copyBtn.setText("Copy");
            copyBtn.setTextColor(Color.WHITE);
            copyBtn.setBackgroundColor(Color.parseColor("#1f2e23"));
            copyBtn.setTextSize(11);
            copyBtn.setPadding(12, 6, 12, 6);
            copyBtn.setOnClickListener(v -> {
                ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
                cm.setPrimaryClip(ClipData.newPlainText("url", url));
                Toast.makeText(this, "Copied: " + url, Toast.LENGTH_SHORT).show();
            });
            card.addView(copyBtn);
        }

        return card;
    }

    private Bitmap generateQR(String data, int size) throws WriterException {
        QRCodeWriter writer = new QRCodeWriter();
        BitMatrix matrix = writer.encode(data, BarcodeFormat.QR_CODE, size, size);
        Bitmap bitmap = Bitmap.createBitmap(size, size, Bitmap.Config.RGB_565);
        for (int x = 0; x < size; x++) {
            for (int y = 0; y < size; y++) {
                bitmap.setPixel(x, y, matrix.get(x, y) ? Color.BLACK : Color.WHITE);
            }
        }
        return bitmap;
    }

    public static String detectArch() {
        String abi = Build.SUPPORTED_ABIS[0];
        if (abi.contains("x86_64")) return "stalkerhek-x86_64";
        else if (abi.contains("x86")) return "stalkerhek-x86";
        else if (abi.contains("arm64")) return "stalkerhek-arm64";
        else return "stalkerhek-arm32";
    }

    private void showPrivacyDialog() {
        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        layout.setGravity(Gravity.CENTER);
        layout.setBackgroundColor(Color.parseColor("#0a0f0a"));
        layout.setPadding(40, 40, 40, 40);

        TextView title = new TextView(this);
        title.setText("Stalkerhek");
        title.setTextColor(Color.parseColor("#e0e6e0"));
        title.setTextSize(22);
        title.setGravity(Gravity.CENTER);
        title.setPadding(0, 0, 0, 16);
        layout.addView(title);

        TextView body = new TextView(this);
        body.setText("Local IPTV proxy server.\n\nNo data collected. All traffic stays on your LAN.\nOpen source: github.com/zetamins/Stalkerheks");
        body.setTextColor(Color.parseColor("#9aaa9a"));
        body.setTextSize(14);
        body.setGravity(Gravity.CENTER);
        body.setPadding(0, 0, 0, 24);
        layout.addView(body);

        Button agree = new Button(this);
        agree.setText("I Understand — Start Server");
        agree.setBackgroundColor(Color.parseColor("#2d7a4e"));
        agree.setTextColor(Color.WHITE);
        agree.setOnClickListener(v -> {
            getSharedPreferences("stalkerhek", MODE_PRIVATE).edit().putBoolean("agreed", true).apply();
            recreate();
        });
        layout.addView(agree);
        setContentView(layout);
    }
}
