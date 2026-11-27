package com.micov.wxxyshall.data.remote

import com.micov.wxxyshall.domain.SchoolErrorKind
import com.micov.wxxyshall.domain.SchoolException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonObject
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class SchoolLoginClientTest {
    private lateinit var server: MockWebServer

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun loginMapsPasswordAndParsesToken() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"code":200}"""))
        server.enqueue(MockResponse().setResponseCode(200).setBody(keyboardBody()))
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"access_token":"school-token","refresh_token":"refresh","expires_in":"604800","sno":"20260001"}""",
            ),
        )
        val client = SchoolLoginClient(
            baseUrl = server.url("/").toString().trimEnd('/'),
            client = SchoolLoginClient.defaultClient(),
        )

        val session = client.login("20260001", "A#1")

        assertEquals("school-token", session.accessToken)
        assertEquals(604800, session.expiresIn)
        server.takeRequest()
        server.takeRequest()
        val tokenRequest = server.takeRequest()
        assertEquals("/berserker-auth/oauth/token", tokenRequest.path)
        val body = tokenRequest.body.readUtf8()
        val form = "http://localhost/?$body".toHttpUrl()
        assertEquals("A#1\$1\$keyboard-1", form.queryParameter("password"))
        assertTrue(tokenRequest.getHeader("Authorization")!!.startsWith("Basic "))
        assertTrue(!body.contains("A#1&"))
    }

    @Test
    fun rejectedCredentialsAreReported() = runTest {
        server.enqueue(MockResponse().setBody("""{"code":200}"""))
        server.enqueue(MockResponse().setBody(keyboardBody()))
        server.enqueue(
            MockResponse().setResponseCode(400)
                .setBody("""{"code":8000,"message":"用户名或密码错误"}"""),
        )
        val client = SchoolLoginClient(
            baseUrl = server.url("/").toString().trimEnd('/'),
            client = SchoolLoginClient.defaultClient(),
        )

        val error = assertThrows(SchoolException::class.java) {
            kotlinx.coroutines.runBlocking { client.login("20260001", "wrong") }
        }
        assertEquals(SchoolErrorKind.CREDENTIALS, error.kind)
    }

    private fun keyboardBody(): String = buildJsonObject {
        put("code", 200)
        putJsonObject("data") {
            put("uuid", "keyboard-1")
            put("numberKeyboard", "0123456789")
            put("lowerLetterKeyboard", "qwertyuiopasdfghjklzxcvbnm")
            put("upperLetterKeyboard", "QWERTYUIOPASDFGHJKLZXCVBNM")
            put("symbolKeyboard", "*\\-[]{}/!,<>?~&@#.:+|`%'\$;^\"_")
        }
    }.toString()
}
