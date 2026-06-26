package com.stalkerhek.app.util

import java.net.Inet4Address
import java.net.NetworkInterface

fun getLocalIpAddress(): String {
    try {
        val interfaces = NetworkInterface.getNetworkInterfaces()
        var bestIp = "127.0.0.1"
        while (interfaces.hasMoreElements()) {
            val netInterface = interfaces.nextElement()
            if (netInterface.isLoopback || !netInterface.isUp) continue
            val addresses = netInterface.inetAddresses
            while (addresses.hasMoreElements()) {
                val addr = addresses.nextElement()
                if (addr is Inet4Address && !addr.isLoopbackAddress) {
                    val ip = addr.hostAddress ?: continue
                    if (ip.startsWith("127.")) continue
                    // Deprioritize only well-known virtual/emulator IPs (the
                    // Android emulator's 10.0.2.x NAT, some tethering bridges).
                    // Do NOT skip generic 10.x or 172.16-31.x — those are valid
                    // Wi-Fi/LAN ranges and skipping them returned the wrong IP
                    // for the dashboard URL on common home networks.
                    if (ip.startsWith("192.168.240.") || ip.startsWith("192.168.250.") ||
                        ip.startsWith("10.0.2.")) {
                        bestIp = ip // keep as fallback
                        continue
                    }
                    // Prefer a routable WiFi/LAN address
                    return ip
                }
            }
        }
        return bestIp
    } catch (_: Exception) {}
    return "127.0.0.1"
}
