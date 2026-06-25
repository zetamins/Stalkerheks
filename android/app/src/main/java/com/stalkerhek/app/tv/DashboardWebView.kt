package com.stalkerhek.app.tv

import android.annotation.SuppressLint
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView

// Always loads 127.0.0.1, never the device's LAN IP. The dashboard server
// runs in this same process (via JNI), so loopback never leaves the device.
// Loading the LAN IP instead would round-trip through the Wi-Fi router (NAT
// hairpin/loopback), which many consumer routers either drop or only
// support over a slow software path — that's what made the on-device
// dashboard slow/unresponsive when opened via the QR code's LAN address.
@SuppressLint("SetJavaScriptEnabled")
@Composable
fun DashboardWebView(onBack: () -> Unit) {
    BackHandler(onBack = onBack)

    Box(modifier = Modifier.fillMaxSize()) {
        AndroidView(factory = { context ->
            WebView(context).apply {
                settings.javaScriptEnabled = true
                settings.domStorageEnabled = true
                webViewClient = object : WebViewClient() {
                    override fun shouldOverrideUrlLoading(view: WebView, url: String): Boolean {
                        // Keep navigation inside the WebView as long as it
                        // stays on the local dashboard.
                        return if (url.startsWith("http://127.0.0.1:8080")) {
                            false
                        } else {
                            true
                        }
                    }
                }
                loadUrl("http://127.0.0.1:8080/")
            }
        })
    }
}
