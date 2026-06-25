package com.stalkerhek.app

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
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
        setContent {
            MaterialTheme {
                StalkerApp()
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
