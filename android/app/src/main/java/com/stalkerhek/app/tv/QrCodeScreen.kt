package com.stalkerhek.app.tv

import android.content.Context
import android.content.Intent
import android.content.res.Configuration
import android.graphics.Bitmap
import android.net.Uri
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
import androidx.compose.ui.platform.LocalContext
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
fun QrCodeScreen() {
    val context = LocalContext.current
    val profile by EngineController.activeProfile.collectAsState()
    val profiles by EngineController.profiles.collectAsState()
    val localIp = remember { getLocalIpAddress() }
    val mgmtPort = 8080
    val mgmtUrl = "http://$localIp:$mgmtPort"
    val isRunning = profile?.running == true

    // Two-column layout is for the TV (landscape) only; phones stay single column.
    val isTv = remember {
        val ui = context.getSystemService(Context.UI_MODE_SERVICE) as android.app.UiModeManager
        ui.currentModeType == Configuration.UI_MODE_TYPE_TELEVISION
    }

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

        val proxyUrl = "http://$localIp:" + (profile?.proxyAddr?.substringAfter(":") ?: "8888")
        val hlsUrl = "http://$localIp:" + (profile?.hlsAddr?.substringAfter(":") ?: "9999")
        val channelsCount = profile?.channelsCount ?: 0

        if (isTv) {
            // TV (landscape): two columns — QR/URL left, status + actions right.
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(40.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                QrSection(mgmtUrl, qrBitmap, Modifier.weight(1f))
                StatusSection(
                    mgmtUrl, isRunning, proxyUrl, hlsUrl, channelsCount,
                    profiles.size, Modifier.weight(1f)
                )
            }
        } else {
            // Phone (portrait): single stacked column.
            QrSection(mgmtUrl, qrBitmap, Modifier.fillMaxWidth())
            Spacer(Modifier.height(24.dp))
            StatusSection(
                mgmtUrl, isRunning, proxyUrl, hlsUrl, channelsCount,
                profiles.size, Modifier.fillMaxWidth()
            )
        }

        Spacer(Modifier.height(32.dp))
    }
}

/** QR code + dashboard URL + scan hint. */
@Composable
private fun QrSection(mgmtUrl: String, qrBitmap: Bitmap?, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
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
    }
}

/** Server status cards + action buttons. */
@Composable
private fun StatusSection(
    mgmtUrl: String,
    isRunning: Boolean,
    proxyUrl: String,
    hlsUrl: String,
    channelsCount: Int,
    profilesCount: Int,
    modifier: Modifier = Modifier
) {
    val context = LocalContext.current
    Column(
        modifier = modifier,
        verticalArrangement = Arrangement.spacedBy(8.dp)
    ) {
        // Server info cards — update dynamically
        InfoRow(label = "Dashboard", value = mgmtUrl)
        if (isRunning) {
            InfoRow(label = "Proxy", value = proxyUrl)
            InfoRow(label = "HLS Stream", value = hlsUrl)
            InfoRow(label = "Profile", value = "Active — $channelsCount channels")
        } else {
            InfoRow(label = "Proxy", value = "Not running")
            InfoRow(
                label = "Profiles",
                value = if (profilesCount == 0) "None configured" else "$profilesCount configured"
            )
        }

        Spacer(Modifier.height(8.dp))

        // Open Dashboard button — launches the device's default browser at
        // 127.0.0.1 (loopback), not the LAN IP, so managing profiles on this
        // device is always fast: the dashboard server runs in this same
        // process via JNI, and loopback never round-trips through the Wi-Fi
        // router (unlike the LAN IP in the QR code, which is for other devices).
        Button(
            onClick = {
                val intent = Intent(Intent.ACTION_VIEW, Uri.parse("http://127.0.0.1:8080/"))
                context.startActivity(intent)
            },
            colors = ButtonDefaults.buttonColors(
                containerColor = Color(0xFF2D8A4E),
                contentColor = Color.White
            ),
            shape = RoundedCornerShape(10.dp),
            modifier = Modifier.fillMaxWidth().height(48.dp)
        ) {
            Text("Open Dashboard", fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
        }

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
            Text("Restart Engine", fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
        }
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
