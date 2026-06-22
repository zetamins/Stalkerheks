package com.stalkerhek.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import com.stalkerhek.app.engine.EngineController
import com.stalkerhek.app.util.getLocalIpAddress

class EngineService : Service() {

    private var wakeLock: PowerManager.WakeLock? = null
    private var isForeground = false

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
        tryStartForeground()
        acquireWakeLock()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                shutdown()
                return START_NOT_STICKY
            }
        }
        val dataDir = filesDir.absolutePath
        EngineController.init(dataDir, this)
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        // If the system kills us (not user-initiated), START_STICKY will restart us
        EngineController.shutdown()
        releaseWakeLock()
        try { stopForeground(STOP_FOREGROUND_REMOVE) } catch (_: Exception) {}
        super.onDestroy()
    }

    private fun shutdown() {
        EngineController.shutdown()
        releaseWakeLock()
        try { stopForeground(STOP_FOREGROUND_REMOVE) } catch (_: Exception) {}
        isForeground = false
        stopSelf()
    }

    private fun tryStartForeground() {
        if (isForeground) return
        try {
            startForeground(NOTIFICATION_ID, buildNotification())
            isForeground = true
        } catch (_: SecurityException) {
            try {
                val minimal = Notification.Builder(this, CHANNEL_ID)
                    .setContentTitle("Stalkerhek")
                    .setContentText("Engine running")
                    .setSmallIcon(android.R.drawable.ic_menu_compass)
                    .setOngoing(true)
                    .build()
                startForeground(NOTIFICATION_ID, minimal)
                isForeground = true
            } catch (_: Exception) {}
        } catch (_: Exception) {}
    }

    private fun acquireWakeLock() {
        try {
            if (wakeLock?.isHeld == true) return
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            wakeLock = pm.newWakeLock(
                PowerManager.PARTIAL_WAKE_LOCK,
                "stalkerhek:engine"
            ).apply {
                setReferenceCounted(false)
                acquire() // hold indefinitely while service runs
            }
        } catch (_: Exception) {}
    }

    private fun releaseWakeLock() {
        try { wakeLock?.release() } catch (_: Exception) {}
        wakeLock = null
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            "Engine Status",
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = "Stalkerhek IPTV proxy"
            setShowBadge(false)
        }
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification {
        val ip = getLocalIpAddress()
        val mgmtUrl = "http://$ip:8080"

        val stopIntent = Intent(this, EngineService::class.java).apply {
            action = ACTION_STOP
        }
        val stopPendingIntent = PendingIntent.getService(
            this, 0, stopIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        return Notification.Builder(this, CHANNEL_ID)
            .setContentTitle("Stalkerhek")
            .setContentText("Dashboard: $mgmtUrl")
            .setSmallIcon(android.R.drawable.ic_menu_compass)
            .setOngoing(true)
            .setShowWhen(false)
            .addAction(android.R.drawable.ic_media_pause, "Stop", stopPendingIntent)
            .build()
    }

    companion object {
        private const val CHANNEL_ID = "stalkerhek_service"
        private const val NOTIFICATION_ID = 1
        const val ACTION_STOP = "com.stalkerhek.app.STOP_ENGINE"
    }
}
