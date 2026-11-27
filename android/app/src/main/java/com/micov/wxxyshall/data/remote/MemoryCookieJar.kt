package com.micov.wxxyshall.data.remote

import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl

class MemoryCookieJar : CookieJar {
    private val mutex = Any()
    private val cookies = mutableMapOf<String, Cookie>()

    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        synchronized(this.cookies) {
            cookies.forEach {
                val key = "${it.name}|${it.domain}|${it.path}"
                this.cookies[key] = it
            }
        }
    }

    override fun loadForRequest(url: HttpUrl): List<Cookie> = synchronized(cookies) {
        val now = System.currentTimeMillis()
        cookies.values.filter { cookie ->
            cookie.expiresAt > now && cookie.matches(url)
        }
    }
}
