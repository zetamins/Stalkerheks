package com.stalkerhek.app.tv

import android.graphics.Bitmap
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.google.zxing.BarcodeFormat
import com.google.zxing.qrcode.QRCodeWriter
import com.stalkerhek.app.engine.EngineController
import com.stalkerhek.app.util.getLocalIpAddress
import kotlinx.coroutines.delay

@Composable
fun QrCodeScreen(onOpenDashboard: () -> Unit) {
    val profile by EngineController.activeProfile.collectAsState()
    val profiles by EngineController.profiles.collectAsState()
    val localIp = remember { getLocalIpAddress() }
    val mgmtPort = 8080
    val mgmtUrl = "http://$localIp:$mgmtPort"
    val isRunning = profile?.running == true
    val scope = rememberCoroutineScope()

    // Auto-refresh profile status every 5s to pick up dashboard changes
    LaunchedEffect(Unit) {
        while (true) {
            delay(5000)
            try {
                EngineController.refresh()
            } catch (_: Exception) {}
        }
    }

    val qrBitmap = remember(mgmtUrl) {
        try {
            val writer = QRCodeWriter()
            val bitMatrix = writer.encode(mgmtUrl, BarcodeFormat.QR_CODE, 512, 512)
            val bitmap = Bitmap.createBitmap(512, 512, Bitmap.Config.RGB_565)
            for (x in 0 until 512) {
                for (y in 0 until 512) {
                    bitmap.setPixel(x, y, if (bitMatrix[x, y]) android.graphics.Color.BLACK else android.graphics.Color.WHITE)
                }
            }
            bitmap
        } catch (_: Exception) { null }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Color(0xFF080C09))
            .verticalScroll(rememberScrollState())
            .padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        Spacer(Modifier.height(16.dp))

        // Logo / Title
        Text(
            "Stalkerhek",
            color = Color(0xFF2D8A4E),
            fontSize = 26.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = (-0.5).sp
        )

        Spacer(Modifier.height(4.dp))

        Text(
            "Management Dashboard",
            color = Color(0xFF8BA38D),
            fontSize = 14.sp,
        )

        Spacer(Modifier.height(28.dp))

        // QR Code in white box
        Box(
            modifier = Modifier
                .size(280.dp)
                .clip(RoundedCornerShape(16.dp))
                .background(Color.White)
                .padding(16.dp),
            contentAlignment = Alignment.Center
        ) {
            qrBitmap?.let {
                Image(
                    bitmap = it.asImageBitmap(),
                    contentDescription = "Dashboard QR Code",
                    modifier = Modifier.fillMaxSize()
                )
            }
        }

        Spacer(Modifier.height(20.dp))

        // Dashboard URL
        Text(
            mgmtUrl,
            color = Color(0xFF2D8A4E),
            fontSize = 22.sp,
            fontWeight = FontWeight.SemiBold,
            textAlign = TextAlign.Center
        )

        Spacer(Modifier.height(4.dp))

        Text(
            "Scan or open in browser to manage profiles",
            color = Color(0xFF6B806D),
            fontSize = 13.sp,
            textAlign = TextAlign.Center
        )

        Spacer(Modifier.height(24.dp))

        // Server info cards — update dynamically
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            InfoRow(label = "Dashboard", value = mgmtUrl)
            if (isRunning) {
                val proxyUrl = "http://$localIp:" + (profile?.proxyAddr?.substringAfter(":") ?: "8888")
                val hlsUrl = "http://$localIp:" + (profile?.hlsAddr?.substringAfter(":") ?: "9999")
                InfoRow(label = "Proxy", value = proxyUrl)
                InfoRow(label = "HLS Stream", value = hlsUrl)
                InfoRow(
                    label = "Profile",
                    value = "Active — ${profile!!.channelsCount} channels"
                )
            } else {
                InfoRow(label = "Proxy", value = "Not running")
                InfoRow(
                    label = "Profiles",
                    value = if (profiles.isEmpty()) "None configured" else "${profiles.size} configured"
                )
            }
        }

        Spacer(Modifier.height(16.dp))

        // Open Dashboard button — loads the dashboard locally via the
        // in-app WebView (127.0.0.1), so managing profiles on this device
        // is always fast. The QR code above is for other devices.
        Button(
            onClick = onOpenDashboard,
            colors = ButtonDefaults.buttonColors(
                containerColor = Color(0xFF2D8A4E),
                contentColor = Color.White
            ),
            shape = RoundedCornerShape(10.dp),
            modifier = Modifier.fillMaxWidth().height(48.dp)
        ) {
            Text(
                "Open Dashboard",
                fontSize = 14.sp,
                fontWeight = FontWeight.SemiBold
            )
        }

        Spacer(Modifier.height(12.dp))

        // Restart Engine button
        Button(
            onClick = { EngineController.restart() },
            colors = ButtonDefaults.buttonColors(
                containerColor = Color(0xFF1A2C1F),
                contentColor = Color(0xFF2D8A4E)
            ),
            shape = RoundedCornerShape(10.dp),
            modifier = Modifier.fillMaxWidth().height(48.dp)
        ) {
            Text(
                "Restart Engine",
                fontSize = 14.sp,
                fontWeight = FontWeight.SemiBold
            )
        }

        Spacer(Modifier.height(32.dp))
    }
}

@Composable
private fun InfoRow(label: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(Color(0xFF0C120E), RoundedCornerShape(10.dp))
            .padding(horizontal = 16.dp, vertical = 10.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(
            label,
            color = Color(0xFF6B806D),
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium
        )
        Text(
            value,
            color = Color(0xFFE2ECE3),
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium
        )
    }
}
