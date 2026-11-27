package com.micov.wxxyshall.data

import com.micov.wxxyshall.data.local.PendingPaymentDao
import com.micov.wxxyshall.data.local.toDomain
import com.micov.wxxyshall.data.local.toEntity
import com.micov.wxxyshall.domain.PendingPayment
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

class PendingPaymentRepository(private val dao: PendingPaymentDao) {
    val latest: Flow<PendingPayment?> = dao.observeLatest().map { it?.toDomain() }

    suspend fun save(payment: PendingPayment) {
        dao.deleteExpired(System.currentTimeMillis())
        dao.upsert(payment.toEntity())
    }

    suspend fun get(orderId: String): PendingPayment? = dao.get(orderId)?.toDomain()

    suspend fun delete(orderId: String) = dao.delete(orderId)

    suspend fun clearExpired() = dao.deleteExpired(System.currentTimeMillis())
}
