package com.micov.wxxyshall.ui

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.micov.wxxyshall.MainActivity
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class LoginScreenTest {
    @get:Rule
    val composeRule = createAndroidComposeRule<MainActivity>()

    @Test
    fun loginScreenRendersForFreshInstall() {
        composeRule.onNodeWithText("宿舍电费").assertIsDisplayed()
        composeRule.onNodeWithText("学校账号").assertIsDisplayed()
        composeRule.onNodeWithText("学校密码").assertIsDisplayed()
    }
}
