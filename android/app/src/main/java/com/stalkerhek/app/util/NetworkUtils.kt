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
                    // Skip common virtual/container IPs
                    if (ip.startsWith("192.168.240.") || ip.startsWith("192.168.250.") ||
                        ip.startsWith("10.0.") || ip.startsWith("172.")) {
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
