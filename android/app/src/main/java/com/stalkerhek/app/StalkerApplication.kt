package com.stalkerhek.app

import android.app.Application

class StalkerApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        // DO NOT start service here — Android 12+ prohibits foreground service
        // starts before the app has a visible activity.
        // The service is started by:
        //   - MainActivity.onCreate() when user opens the app
        //   - BootReceiver.onReceive() when device boots
    }

    override fun onTerminate() {
        com.stalkerhek.app.engine.EngineController.shutdown()
        super.onTerminate()
    }
}
