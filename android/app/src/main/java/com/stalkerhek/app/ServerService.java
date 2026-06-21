package com.stalkerhek.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.os.Build;
import android.os.IBinder;
import android.util.Log;

import com.stalkerhek.app.engine.EngineBridge;

import java.net.Inet4Address;
import java.net.NetworkInterface;

public class ServerService extends Service {

    private static final String CHANNEL_ID = "stalkerhek_engine";
    private static final int NOTIFY_ID = 1;

    @Override
    public void onCreate() {
        super.onCreate();
        createNotificationChannel();
        try {
            startForeground(NOTIFY_ID, buildNotification());
        } catch (SecurityException e) {
            // Notification permission not granted — run anyway
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String dataDir = getFilesDir().getAbsolutePath();
        new Thread(() -> {
            try {
                // Use JNI on ARM64, exec fallback on other ABIs
                if (isArm64()) {
                    Log.i("Stalkerhek", "JNI init: " + dataDir);
                    String result = EngineBridge.nativeInit(dataDir);
                    Log.i("Stalkerhek", "JNI result: " + result);
                } else {
                    Log.i("Stalkerhek", "Exec init: " + dataDir);
                    String binPath = getApplicationInfo().nativeLibraryDir + "/libstalkerhek.so";
                    Runtime.getRuntime().exec(new String[]{binPath, "-profile", "default", "-db", dataDir});
                }
            } catch (Exception e) {
                Log.e("Stalkerhek", "Engine start failed", e);
            }
        }).start();
        return START_STICKY;
    }

    @Override
    public IBinder onBind(Intent intent) { return null; }

    private void createNotificationChannel() {
        NotificationChannel channel = new NotificationChannel(
            CHANNEL_ID, "Stalkerhek", NotificationManager.IMPORTANCE_LOW);
        channel.setDescription("IPTV proxy running");
        NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        nm.createNotificationChannel(channel);
    }

    private Notification buildNotification() {
        String ip = getLocalIp();
        String mgmtUrl = "http://" + ip + ":8080";
        return new Notification.Builder(this, CHANNEL_ID)
            .setContentTitle("Stalkerhek")
            .setContentText("Dashboard: " + mgmtUrl)
            .setSmallIcon(android.R.drawable.ic_menu_compass)
            .setOngoing(true)
            .build();
    }

    private boolean isArm64() {
        return Build.SUPPORTED_ABIS.length > 0 && Build.SUPPORTED_ABIS[0].contains("arm64");
    }

    private String getLocalIp() {
        try {
            for (NetworkInterface iface : java.util.Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!iface.isLoopback() && iface.isUp()) {
                    for (java.net.InetAddress a : java.util.Collections.list(iface.getInetAddresses())) {
                        if (a instanceof Inet4Address) return a.getHostAddress();
                    }
                }
            }
        } catch (Exception e) {}
        return "127.0.0.1";
    }
}
