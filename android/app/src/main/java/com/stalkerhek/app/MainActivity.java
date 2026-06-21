package com.stalkerhek.app;

import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.content.res.ColorStateList;
import android.graphics.Bitmap;
import android.graphics.Color;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.widget.Button;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import androidx.appcompat.app.AppCompatActivity;

import com.google.android.material.button.MaterialButton;
import com.google.android.material.card.MaterialCardView;
import com.google.zxing.BarcodeFormat;
import com.google.zxing.common.BitMatrix;
import com.google.zxing.qrcode.QRCodeWriter;

import java.net.Inet4Address;
import java.net.NetworkInterface;

public class MainActivity extends AppCompatActivity {

    private String host = "127.0.0.1";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getSharedPreferences("stalkerhek", MODE_PRIVATE).edit().putBoolean("agreed", true).apply();

        detectWifiIp();
        startServerService();

        // Loading screen
        LinearLayout loading = new LinearLayout(this);
        loading.setOrientation(LinearLayout.VERTICAL);
        int pad = dp(32);
        loading.setPadding(pad, pad * 3, pad, pad);
        loading.setGravity(android.view.Gravity.CENTER);

        com.google.android.material.progressindicator.CircularProgressIndicator spinner =
            new com.google.android.material.progressindicator.CircularProgressIndicator(this);
        spinner.setIndeterminate(true);
        spinner.setIndicatorColor(getColor(android.R.color.system_accent1_200));
        spinner.setTrackThickness(dp(3));
        spinner.setLayoutParams(new LinearLayout.LayoutParams(dp(48), dp(48)));
        loading.addView(spinner);

        TextView status = new TextView(this);
        status.setText("Starting server...");
        status.setTextColor(Color.parseColor("#E0E6E0"));
        status.setTextSize(16);
        status.setPadding(0, dp(20), 0, 0);
        loading.addView(status);

        setContentView(loading);
        new Handler(Looper.getMainLooper()).postDelayed(this::showMainScreen, 8000);
    }

    private void showMainScreen() {
        String proxyURL = "http://" + host + ":8888/c/";
        String hlsURL = "http://" + host + ":9999/iptv/";
        String dashURL = "http://" + host + ":8080/";

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        int pad = dp(20);
        root.setPadding(pad, dp(40), pad, pad);

        // Header card
        MaterialCardView headerCard = new MaterialCardView(this);
        headerCard.setCardElevation(0);
        headerCard.setStrokeWidth(0);
        headerCard.setRadius(dp(16));
        headerCard.setContentPadding(pad, pad, pad, pad);
        LinearLayout headerLayout = new LinearLayout(this);
        headerLayout.setOrientation(LinearLayout.VERTICAL);
        headerLayout.setGravity(android.view.Gravity.CENTER);

        TextView title = new TextView(this);
        title.setText("Stalkerhek");
        title.setTextSize(24);
        title.setTextColor(Color.parseColor("#E0E6E0"));
        title.setPadding(0, 0, 0, dp(4));
        headerLayout.addView(title);

        TextView subtitle = new TextView(this);
        subtitle.setText("Server running");
        subtitle.setTextSize(14);
        subtitle.setTextColor(Color.parseColor("#3FB970"));
        headerLayout.addView(subtitle);

        headerCard.addView(headerLayout);
        root.addView(headerCard);

        // QR code
        try {
            Bitmap qr = generateQR(dashURL, dp(200));
            ImageView qrView = new ImageView(this);
            qrView.setImageBitmap(qr);
            qrView.setPadding(0, dp(20), 0, dp(8));
            LinearLayout.LayoutParams qrParams = new LinearLayout.LayoutParams(dp(200), dp(200));
            qrParams.gravity = android.view.Gravity.CENTER;
            qrView.setLayoutParams(qrParams);
            root.addView(qrView);
        } catch (Exception e) {}

        TextView qrHint = new TextView(this);
        qrHint.setText("Scan to open dashboard");
        qrHint.setTextSize(12);
        qrHint.setTextColor(Color.parseColor("#9AAAA9"));
        qrHint.setPadding(0, 0, 0, dp(20));
        qrHint.setGravity(android.view.Gravity.CENTER);
        root.addView(qrHint);

        // URL cards
        root.addView(urlCard("Dashboard", dashURL, "Open in browser"));
        root.addView(urlCard("STB Proxy", proxyURL, "Set in MAG portal URL"));
        root.addView(urlCard("HLS Streams", hlsURL + "<channel>", "Play in VLC"));

        // Spacer
        android.view.View spacer = new android.view.View(this);
        spacer.setLayoutParams(new LinearLayout.LayoutParams(1, dp(24)));
        root.addView(spacer);

        // Buttons
        MaterialButton restartBtn = new MaterialButton(this);
        restartBtn.setText("Restart Server");
        restartBtn.setBackgroundColor(Color.parseColor("#2D7A4E"));
        restartBtn.setTextColor(Color.WHITE);
        restartBtn.setStrokeWidth(0);
        restartBtn.setCornerRadius(dp(12));
        restartBtn.setLayoutParams(new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT));
        restartBtn.setOnClickListener(v -> {
            stopService(new Intent(this, ServerService.class));
            new Handler().postDelayed(() -> {
                Intent s = new Intent(this, ServerService.class);
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(s);
                else startService(s);
                Toast.makeText(this, "Server restarted", Toast.LENGTH_SHORT).show();
            }, 1500);
        });
        root.addView(restartBtn);

        MaterialButton killBtn = new MaterialButton(this, null, com.google.android.material.R.attr.materialButtonOutlinedStyle);
        killBtn.setText("Stop Server");
        killBtn.setTextColor(Color.parseColor("#E85D4D"));
        killBtn.setStrokeColor(ColorStateList.valueOf(Color.parseColor("#E85D4D")));
        killBtn.setStrokeWidth(dp(1));
        killBtn.setCornerRadius(dp(12));
        LinearLayout.LayoutParams kp = new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        kp.setMargins(0, dp(8), 0, 0);
        killBtn.setLayoutParams(kp);
        killBtn.setOnClickListener(v -> {
            stopService(new Intent(this, ServerService.class));
            Toast.makeText(this, "Server stopped", Toast.LENGTH_SHORT).show();
            finish();
        });
        root.addView(killBtn);

        android.widget.ScrollView scroll = new android.widget.ScrollView(this);
        scroll.addView(root);
        setContentView(scroll);
    }

    private MaterialCardView urlCard(String label, String url, String hint) {
        MaterialCardView card = new MaterialCardView(this);
        card.setCardElevation(0);
        card.setStrokeWidth(dp(1));
        card.setStrokeColor(ColorStateList.valueOf(Color.parseColor("#1F2E23")));
        card.setRadius(dp(12));
        card.setContentPadding(dp(14), dp(10), dp(14), dp(10));
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        params.setMargins(0, 0, 0, dp(8));
        card.setLayoutParams(params);

        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(android.view.Gravity.CENTER_VERTICAL);

        LinearLayout textCol = new LinearLayout(this);
        textCol.setOrientation(LinearLayout.VERTICAL);
        textCol.setLayoutParams(new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        TextView labelView = new TextView(this);
        labelView.setText(label);
        labelView.setTextColor(Color.parseColor("#E0E6E0"));
        labelView.setTextSize(14);
        textCol.addView(labelView);

        TextView urlView = new TextView(this);
        urlView.setText(url);
        urlView.setTextColor(Color.parseColor("#3FB970"));
        urlView.setTextSize(12);
        urlView.setPadding(0, dp(2), 0, 0);
        textCol.addView(urlView);

        TextView hintView = new TextView(this);
        hintView.setText(hint);
        hintView.setTextColor(Color.parseColor("#9AAAA9"));
        hintView.setTextSize(11);
        textCol.addView(hintView);

        row.addView(textCol);

        Button copyBtn = new Button(this);
        copyBtn.setText("Copy");
        copyBtn.setTextColor(Color.parseColor("#3FB970"));
        copyBtn.setBackgroundColor(Color.TRANSPARENT);
        copyBtn.setTextSize(12);
        copyBtn.setOnClickListener(v -> {
            ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
            cm.setPrimaryClip(ClipData.newPlainText("url", url));
            Toast.makeText(this, "Copied", Toast.LENGTH_SHORT).show();
        });
        row.addView(copyBtn);

        card.addView(row);
        return card;
    }

    private void detectWifiIp() {
        try {
            for (NetworkInterface iface : java.util.Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!iface.isLoopback() && iface.isUp()) {
                    for (java.net.InetAddress addr : java.util.Collections.list(iface.getInetAddresses())) {
                        if (addr instanceof Inet4Address) { host = addr.getHostAddress(); return; }
                    }
                }
            }
        } catch (Exception e) {}
    }

    private void startServerService() {
        Intent si = new Intent(this, ServerService.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(si);
        else startService(si);
    }

    private Bitmap generateQR(String data, int size) throws Exception {
        BitMatrix matrix = new QRCodeWriter().encode(data, BarcodeFormat.QR_CODE, size, size);
        Bitmap bmp = Bitmap.createBitmap(size, size, Bitmap.Config.RGB_565);
        for (int x = 0; x < size; x++)
            for (int y = 0; y < size; y++)
                bmp.setPixel(x, y, matrix.get(x, y) ? Color.BLACK : Color.WHITE);
        return bmp;
    }

    private int dp(int px) { return (int) (px * getResources().getDisplayMetrics().density); }
}
