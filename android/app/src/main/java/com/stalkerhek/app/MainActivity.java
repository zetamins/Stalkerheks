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
import android.widget.Button;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.TextView;
import android.widget.Toast;
import android.widget.ScrollView;

import com.google.zxing.BarcodeFormat;
import com.google.zxing.common.BitMatrix;
import com.google.zxing.qrcode.QRCodeWriter;

import java.net.Inet4Address;
import java.net.NetworkInterface;

public class MainActivity extends Activity {

    private String host = "127.0.0.1";

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getSharedPreferences("stalkerhek", MODE_PRIVATE).edit().putBoolean("agreed", true).apply();

        // Detect WiFi IP
        try {
            for (NetworkInterface iface : java.util.Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!iface.isLoopback() && iface.isUp()) {
                    for (java.net.InetAddress a : java.util.Collections.list(iface.getInetAddresses())) {
                        if (a instanceof Inet4Address) { host = a.getHostAddress(); break; }
                    }
                }
            }
        } catch (Exception e) {}

        // Show loading
        LinearLayout load = new LinearLayout(this);
        load.setOrientation(LinearLayout.VERTICAL);
        load.setGravity(Gravity.CENTER);
        load.setBackgroundColor(Color.parseColor("#0a0f0a"));
        ProgressBar sp = new ProgressBar(this);
        sp.getIndeterminateDrawable().setTint(Color.parseColor("#2d7a4e"));
        load.addView(sp);
        TextView st = new TextView(this);
        st.setText("Starting server...");
        st.setTextColor(Color.WHITE);
        st.setTextSize(16);
        st.setPadding(0, 20, 0, 0);
        load.addView(st);
        setContentView(load);

        // Start service
        Intent si = new Intent(this, ServerService.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(si);
        else startService(si);

        // Show UI after delay
        new Handler(Looper.getMainLooper()).postDelayed(this::showUI, 8000);
    }

    private void showUI() {
        String proxyURL = "http://" + host + ":8888/c/";
        String hlsURL = "http://" + host + ":9999/iptv/";
        String dashURL = "http://" + host + ":8080/";

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setGravity(Gravity.CENTER_HORIZONTAL);
        root.setBackgroundColor(Color.parseColor("#0a0f0a"));
        root.setPadding(32, 48, 32, 32);

        // Header: green dot + title
        LinearLayout hdr = new LinearLayout(this);
        hdr.setGravity(Gravity.CENTER);
        hdr.setOrientation(LinearLayout.HORIZONTAL);
        GradientDrawable dot = new GradientDrawable();
        dot.setShape(GradientDrawable.OVAL);
        dot.setColor(0xFF3fb970);
        dot.setSize(14, 14);
        android.view.View dv = new android.view.View(this);
        dv.setBackground(dot);
        dv.setLayoutParams(new LinearLayout.LayoutParams(14, 14));
        hdr.addView(dv);
        TextView ti = new TextView(this);
        ti.setText("  Stalkerhek");
        ti.setTextColor(Color.WHITE);
        ti.setTextSize(20);
        hdr.addView(ti);
        root.addView(hdr);

        TextView pf = new TextView(this);
        pf.setText("Server running on " + host);
        pf.setTextColor(Color.parseColor("#9aaa9a"));
        pf.setTextSize(13);
        pf.setPadding(0, 8, 0, 20);
        root.addView(pf);

        // QR code
        try {
            Bitmap qr = generateQR(dashURL, 220);
            ImageView qv = new ImageView(this);
            qv.setImageBitmap(qr);
            qv.setPadding(0, 0, 0, 8);
            root.addView(qv);
        } catch (Exception e) {}

        TextView ql = new TextView(this);
        ql.setText("Scan to open dashboard");
        ql.setTextColor(Color.parseColor("#6b7280"));
        ql.setTextSize(12);
        ql.setPadding(0, 4, 0, 20);
        root.addView(ql);

        // URL cards
        root.addView(urlCard("Dashboard", dashURL));
        root.addView(urlCard("STB Proxy", proxyURL));
        root.addView(urlCard("HLS Streams", hlsURL + "<channel>"));

        // Spacer
        android.view.View sp = new android.view.View(this);
        sp.setLayoutParams(new LinearLayout.LayoutParams(1, 20));
        root.addView(sp);

        // Restart
        Button rb = new Button(this);
        rb.setText("Restart Server");
        rb.setTextColor(Color.WHITE);
        rb.setBackgroundColor(Color.parseColor("#2d7a4e"));
        rb.setOnClickListener(v -> {
            stopService(new Intent(this, ServerService.class));
            new Handler().postDelayed(() -> {
                Intent s = new Intent(this, ServerService.class);
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) startForegroundService(s);
                else startService(s);
                Toast.makeText(this, "Server restarted", Toast.LENGTH_SHORT).show();
            }, 1500);
        });
        root.addView(rb);

        // Kill
        Button kb = new Button(this);
        kb.setText("Kill Server");
        kb.setTextColor(Color.WHITE);
        kb.setBackgroundColor(Color.parseColor("#e85d4d"));
        kb.setOnClickListener(v -> {
            stopService(new Intent(this, ServerService.class));
            Toast.makeText(this, "Server stopped", Toast.LENGTH_SHORT).show();
            finish();
        });
        root.addView(kb);

        ScrollView sv = new ScrollView(this);
        sv.addView(root);
        sv.setBackgroundColor(Color.parseColor("#0a0f0a"));
        setContentView(sv);
    }

    private LinearLayout urlCard(String label, String url) {
        LinearLayout card = new LinearLayout(this);
        card.setOrientation(LinearLayout.HORIZONTAL);
        card.setGravity(Gravity.CENTER_VERTICAL);
        card.setBackgroundColor(Color.parseColor("#0d1410"));
        card.setPadding(14, 10, 14, 10);
        LinearLayout.LayoutParams p = new LinearLayout.LayoutParams(
            LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        p.setMargins(0, 0, 0, 8);
        card.setLayoutParams(p);

        LinearLayout tc = new LinearLayout(this);
        tc.setOrientation(LinearLayout.VERTICAL);
        tc.setLayoutParams(new LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f));
        TextView lb = new TextView(this);
        lb.setText(label);
        lb.setTextColor(Color.parseColor("#9aaa9a"));
        lb.setTextSize(11);
        tc.addView(lb);
        TextView vl = new TextView(this);
        vl.setText(url);
        vl.setTextColor(Color.parseColor("#3fb970"));
        vl.setTextSize(13);
        vl.setMovementMethod(LinkMovementMethod.getInstance());
        tc.addView(vl);
        card.addView(tc);

        Button cb = new Button(this);
        cb.setText("Copy");
        cb.setTextColor(Color.WHITE);
        cb.setBackgroundColor(Color.parseColor("#1f2e23"));
        cb.setTextSize(11);
        cb.setOnClickListener(v -> {
            ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
            cm.setPrimaryClip(ClipData.newPlainText("url", url));
            Toast.makeText(this, "Copied!", Toast.LENGTH_SHORT).show();
        });
        card.addView(cb);
        return card;
    }

    private Bitmap generateQR(String data, int size) throws Exception {
        BitMatrix matrix = new QRCodeWriter().encode(data, BarcodeFormat.QR_CODE, size, size);
        Bitmap bmp = Bitmap.createBitmap(size, size, Bitmap.Config.RGB_565);
        for (int x = 0; x < size; x++)
            for (int y = 0; y < size; y++)
                bmp.setPixel(x, y, matrix.get(x, y) ? Color.BLACK : Color.WHITE);
        return bmp;
    }
}
