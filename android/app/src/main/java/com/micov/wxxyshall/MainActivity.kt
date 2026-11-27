package com.micov.wxxyshall

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.fragment.app.FragmentActivity
import com.micov.wxxyshall.ui.WxxyshallRoot
import com.micov.wxxyshall.ui.theme.WxxyshallTheme
import kotlinx.coroutines.flow.MutableStateFlow

class MainActivity : FragmentActivity() {
    val warmDeepLink = MutableStateFlow<Uri?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            WxxyshallTheme {
                WxxyshallRoot(this)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        warmDeepLink.value = intent.data
    }

    fun consumeWarmDeepLink() {
        warmDeepLink.value = null
    }
}
