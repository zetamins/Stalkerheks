package com.stalkerhek.app

import android.Manifest
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.core.content.ContextCompat
import com.stalkerhek.app.engine.EngineController
import com.stalkerhek.app.engine.EngineState
import com.stalkerhek.app.tv.QrCodeScreen
import kotlinx.coroutines.delay

class MainActivity : ComponentActivity() {

    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { _ ->
        // Restart the service now that permission may be granted
        startEngineService()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Request notification permission on Android 13+ (required for foreground service)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED
            ) {
                notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            }
        }

        startEngineService()
        requestBatteryExemption()
        setContent {
            MaterialTheme {
                StalkerApp()
            }
        }
    }

    /**
     * Ask the OS to exempt this app from battery optimization so the foreground
     * engine isn't frozen in the background — the #1 reason the proxy/dashboard
     * ports take minutes to respond on aggressive-power-management devices
     * (Huawei/EMUI, Xiaomi, etc.). Once granted, the system never asks again.
     * On Huawei specifically the standard exemption often isn't enough, so we
     * also try to open the EMUI "protected apps" startup manager.
     */
    private fun requestBatteryExemption() {
        try {
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            if (pm.isIgnoringBatteryOptimizations(packageName)) return

            val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
                data = Uri.parse("package:$packageName")
            }
            if (intent.resolveActivity(packageManager) != null) {
                startActivity(intent)
            } else {
                // Fallback: open the generic battery-optimization list.
                startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
            }
        } catch (e: Exception) {
            Log.w("Stalkerhek", "Battery exemption request failed", e)
        }

        // EMUI/HarmonyOS "protected apps" / startup manager — best-effort, the
        // component name differs across versions so failures are expected and
        // ignored. Only attempted on Huawei devices.
        if (Build.MANUFACTURER.equals("HUAWEI", ignoreCase = true) ||
            Build.MANUFACTURER.equals("HONOR", ignoreCase = true)
        ) {
            for (cn in HUAWEI_PROTECTED_APPS_COMPONENTS) {
                try {
                    startActivity(Intent().apply { component = cn })
                    break
                } catch (_: Exception) {
                    // try next known component
                }
            }
        }
    }

    private fun startEngineService() {
        val si = Intent(this, EngineService::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(si)
        } else {
            startService(si)
        }
    }

    companion object {
        // Known EMUI/HarmonyOS startup-manager activities across versions; the
        // first that resolves is opened so the user can allow auto-launch /
        // background running. Component names vary by version, hence the list.
        private val HUAWEI_PROTECTED_APPS_COMPONENTS = listOf(
            ComponentName(
                "com.huawei.systemmanager",
                "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"
            ),
            ComponentName(
                "com.huawei.systemmanager",
                "com.huawei.systemmanager.appcontrol.activity.StartupAppControlActivity"
            ),
            ComponentName(
                "com.huawei.systemmanager",
                "com.huawei.systemmanager.optimize.process.ProtectActivity"
            ),
        )
    }
}

@Composable
fun StalkerApp() {
    val state by EngineController.engineState.collectAsState()

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Color(0xFF080C09)),
        contentAlignment = Alignment.Center
    ) {
        when (state) {
            is EngineState.Uninitialized, is EngineState.Initializing -> {
                LoadingScreen()
            }
            is EngineState.Ready -> {
                QrCodeScreen()
            }
            is EngineState.Error -> {
                val error = (state as EngineState.Error).message
                ErrorScreen(error)
            }
        }
    }
}

@Composable
fun LoadingScreen() {
    var dotCount by remember { mutableIntStateOf(0) }

    LaunchedEffect(Unit) {
        while (true) {
            delay(500)
            dotCount = (dotCount + 1) % 4
        }
    }

    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center
    ) {
        // Pulse dots
        val dots = listOf(
            Color(0xFF2D8A4E),
            Color(0xFF2D8A4E).copy(alpha = 0.6f),
            Color(0xFF2D8A4E).copy(alpha = 0.3f),
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            dots.forEachIndexed { i, color ->
                Box(
                    modifier = Modifier
                        .size(if (i == 0) 14.dp else 10.dp)
                        .clip(RoundedCornerShape(50))
                        .background(color)
                )
            }
        }

        Spacer(Modifier.height(32.dp))

        Text(
            "Stalkerhek",
            color = Color(0xFF2D8A4E),
            fontSize = 28.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = (-0.5).sp
        )

        Spacer(Modifier.height(8.dp))

        val loadingText = "Initializing engine" + ".".repeat(dotCount)
        Text(
            loadingText,
            color = Color(0xFF8BA38D),
            fontSize = 15.sp
        )
    }
}

@Composable
fun ErrorScreen(error: String) {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
        modifier = Modifier.padding(32.dp)
    ) {
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(RoundedCornerShape(50))
                .background(Color(0xFF3D1A1A)),
            contentAlignment = Alignment.Center
        ) {
            Text("!", color = Color(0xFFE85D4D), fontSize = 24.sp, fontWeight = FontWeight.Bold)
        }

        Spacer(Modifier.height(20.dp))

        Text(
            "Engine Error",
            color = Color(0xFFE85D4D),
            fontSize = 20.sp,
            fontWeight = FontWeight.SemiBold
        )

        Spacer(Modifier.height(8.dp))

        Text(
            error,
            color = Color(0xFF8BA38D),
            fontSize = 14.sp,
            textAlign = TextAlign.Center
        )
    }
}
