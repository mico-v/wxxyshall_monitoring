package com.micov.wxxyshall.data.remote

import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import com.micov.wxxyshall.domain.PaymentMethod
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

class SchoolChargeClientTest {
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
    fun establishesCookieSessionThroughSameOriginRedirect() = runTest {
        server.enqueue(
            MockResponse().setResponseCode(302)
                .addHeader("Location", server.url("/charge-app/")),
        )
        server.enqueue(MockResponse().setBody("ok"))
        server.enqueue(MockResponse().setBody("ok"))
        server.enqueue(
            MockResponse().setBody(
                """
                {"code":200,"map":{
                  "surplusCharge":"23.45",
                  "showData":{"电表总用电量":"88.8"},
                  "data":{"campus":"东校区","building":"1号楼","room":"502"}
                }}
                """.trimIndent(),
            ),
        )
        val pacer = RequestPacer(perMinute = 600, clock = { 1L }, sleep = {})
        val client = SchoolChargeClient(
            baseUrl = server.url("/").toString().trimEnd('/'),
            token = "school-token",
            pacer = pacer,
            client = SchoolChargeClient.defaultClient(),
        )

        val snapshot = client.queryRoomSnapshot("campus", "building", "room")

        assertEquals(23.45, snapshot.reading.surplusCharge!!, 0.001)
        assertEquals("88.8", snapshot.reading.show["电表总用电量"])
        assertEquals("502", snapshot.roomName)
        val first = server.takeRequest()
        val second = server.takeRequest()
        val third = server.takeRequest()
        val fourth = server.takeRequest()
        assertEquals("/charge/feeitem/toAppitem", first.path.orEmpty().substringBefore('?'))
        assertEquals("/charge-app/", second.path)
        assertEquals("/charge/user/caslogin_userxx", third.path)
        assertEquals("/charge/feeitem/getThirdData", fourth.path)
    }

    @Test
    fun parsesTenDigitPasswordMapString() = runTest {
        server.enqueue(
            MockResponse().setResponseCode(302)
                .addHeader("Location", server.url("/charge-app/")),
        )
        server.enqueue(MockResponse().setBody("ok"))
        server.enqueue(MockResponse().setBody("ok"))
        server.enqueue(
            MockResponse().setBody(
                """{"code":200,"data":{"passwordMap":{"uuid-4":"1459782630"}}}""",
            ),
        )
        val client = SchoolChargeClient(
            baseUrl = server.url("/").toString().trimEnd('/'),
            token = "school-token",
            pacer = RequestPacer(perMinute = 600, clock = { 1L }, sleep = {}),
            client = SchoolChargeClient.defaultClient(),
        )

        val passwordMap = client.getPasswordMap(
            orderId = "order-id",
            method = PaymentMethod(1, "卡账户", "CARDTSM", 0, false),
        )

        assertEquals("uuid-4", passwordMap.uuid)
        assertEquals(listOf(1, 4, 5, 9, 7, 8, 2, 6, 3, 0), passwordMap.keys)
    }

    @Test
    fun cancelsOrderWithJsonBody() = runTest {
        server.enqueue(
            MockResponse().setResponseCode(302)
                .addHeader("Location", server.url("/charge-app/")),
        )
        server.enqueue(MockResponse().setBody("ok"))
        server.enqueue(MockResponse().setBody("ok"))
        server.enqueue(MockResponse().setBody("""{"code":200,"msg":"success"}"""))
        val client = SchoolChargeClient(
            baseUrl = server.url("/").toString().trimEnd('/'),
            token = "school-token",
            pacer = RequestPacer(perMinute = 600, clock = { 1L }, sleep = {}),
            client = SchoolChargeClient.defaultClient(),
        )

        client.cancelOrder("order-id")

        server.takeRequest()
        server.takeRequest()
        server.takeRequest()
        val cancel = server.takeRequest()
        assertEquals("application/json", cancel.getHeader("Content-Type")?.substringBefore(';'))
        assertEquals("""{"orderid":"order-id"}""", cancel.body.readUtf8())
    }
}
