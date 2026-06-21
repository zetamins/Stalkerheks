package com.stalkerhek.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.os.Build;
import android.os.IBinder;

/**
 * Foreground service that keeps the stalkerhek Go server alive.
 * Shows a persistent notification while running.
 */
public class ServerService extends Service {

    private static final String CHANNEL_ID = "stalkerhek_server";
    private static final int NOTIFY_ID = 1;
    private Process serverProcess;

    @Override
    public void onCreate() {
        super.onCreate();
        createNotificationChannel();
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        Notification notification = new Notification.Builder(this, CHANNEL_ID)
            .setContentTitle("Stalkerhek Server")
            .setContentText("IPTV proxy running — tap to open")
            .setSmallIcon(android.R.drawable.ic_menu_manage)
            .setContentIntent(PendingIntent.getActivity(this, 0,
                new Intent(this, MainActivity.class),
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT))
            .setOngoing(true)
            .build();

        startForeground(NOTIFY_ID, notification);

        // Use native library directory — Android grants exec permission here.
        // The binary is bundled as libstalkerhek.so in jniLibs/<abi>/.
        final String dbDir = getFilesDir().getAbsolutePath();
        final String binPath = getApplicationInfo().nativeLibraryDir + "/libstalkerhek.so";
        final String dbPath = dbDir + "/stalkerhek.db";

        // Pre-create database with default profile so the server can start
        try {
            java.io.File dbFile = new java.io.File(dbPath);
            if (!dbFile.exists()) {
                String defaultProfile = "{\"default\":{\"name\":\"default\",\"portal\":{\"model\":\"MAG254\",\"serial_number\":\"0000000000000\",\"device_id\":\"0000000000000000000000000000000000000000000000000000000000000000\",\"device_id2\":\"0000000000000000000000000000000000000000000000000000000000000000\",\"signature\":\"0000000000000000000000000000000000000000000000000000000000000000\",\"mac\":\"00:00:00:00:00:00\",\"url\":\"http://example.com/c/\",\"time_zone\":\"Europe/London\",\"token\":\"\"},\"services\":{\"proxy_bind\":\"0.0.0.0:8888\",\"hls_bind\":\"0.0.0.0:9999\"},\"dashboard\":{\"bind\":\"0.0.0.0:8080\",\"profiles_dir\":\".\"}}}";
                java.io.FileOutputStream fos = new java.io.FileOutputStream(dbFile);
                fos.write(defaultProfile.getBytes());
                fos.close();
                android.util.Log.i("Stalkerhek", "Default DB created at " + dbPath);
            }
        } catch (Exception e) {
            android.util.Log.e("Stalkerhek", "DB creation failed", e);
        }

        new Thread(() -> {
            try {
                android.util.Log.i("Stalkerhek", "Binary: " + binPath + " exists=" + new java.io.File(binPath).exists());
                android.util.Log.i("Stalkerhek", "Starting: " + binPath + " -profile default -db " + dbDir);
                serverProcess = Runtime.getRuntime().exec(
                    new String[]{binPath, "-profile", "default", "-db", dbDir});

                // Log output
                java.io.BufferedReader reader = new java.io.BufferedReader(
                    new java.io.InputStreamReader(serverProcess.getInputStream()));
                String line;
                while ((line = reader.readLine()) != null) {
                    android.util.Log.i("Stalkerhek", line);
                }
            } catch (Exception e) {
                android.util.Log.e("Stalkerhek", "Server start failed", e);
            }
        }).start();

        return START_STICKY;
    }

    @Override
    public IBinder onBind(Intent intent) { return null; }

    @Override
    public void onDestroy() {
        if (serverProcess != null) {
            serverProcess.destroy();
        }
        super.onDestroy();
    }

    private String detectArch() {
        String abi = Build.SUPPORTED_ABIS[0];
        if (abi.contains("x86_64")) return "stalkerhek-x86_64";
        else if (abi.contains("x86")) return "stalkerhek-x86";
        else if (abi.contains("arm64")) return "stalkerhek-arm64";
        else return "stalkerhek-arm32";
    }

    private void createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(
                CHANNEL_ID, "Stalkerhek Server",
                NotificationManager.IMPORTANCE_LOW);
            channel.setDescription("Persistent notification while server is running");
            NotificationManager nm = getSystemService(NotificationManager.class);
            nm.createNotificationChannel(channel);
        }
    }
}
