const DB_NAME = 'elec-user-portal';
const DB_VERSION = 1;
const SESSION_KEY_ID = 'session-key';
const SESSION_ID = 'session';

export interface UserSession {
  sno: string;
  accessToken: string;
  expiresIn: number;
  loginTime: number;
  source: string;
  monitorAccount: boolean;
}

interface StoredKey {
  id: string;
  key: CryptoKey;
}

interface StoredSession {
  id: string;
  iv: ArrayBuffer;
  cipher: ArrayBuffer;
  sno: string;
  expiresIn: number;
  loginTime: number;
  source: string;
  monitorAccount?: boolean;
}

interface CachedValue<T> {
  key: string;
  value: T;
  expiresAt: number;
}

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === 'undefined') {
      reject(new Error('当前浏览器不支持 IndexedDB'));
      return;
    }
    const request = indexedDB.open(DB_NAME, DB_VERSION);
    request.onerror = () => reject(request.error || new Error('打开本地数据库失败'));
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains('state')) db.createObjectStore('state', { keyPath: 'id' });
      if (!db.objectStoreNames.contains('cache')) {
        db.createObjectStore('cache', { keyPath: 'key' });
      }
    };
    request.onsuccess = () => resolve(request.result);
  });
}

function requestAsPromise<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error('本地数据库操作失败'));
  });
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error || new Error('本地数据库事务失败'));
    transaction.onabort = () => reject(transaction.error || new Error('本地数据库事务已取消'));
  });
}

function bytes(value: ArrayBuffer): Uint8Array<ArrayBuffer> {
  return new Uint8Array(value);
}

export async function saveUserSession(session: UserSession): Promise<void> {
  const db = await openDatabase();
  try {
    const key = await crypto.subtle.generateKey(
      { name: 'AES-GCM', length: 256 },
      false,
      ['encrypt', 'decrypt'],
    );
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const cipher = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv },
      key,
      new TextEncoder().encode(session.accessToken),
    );
    const transaction = db.transaction('state', 'readwrite');
    const store = transaction.objectStore('state');
    store.put({ id: SESSION_KEY_ID, key } satisfies StoredKey);
    store.put({
      id: SESSION_ID,
      iv: iv.buffer,
      cipher,
      sno: session.sno,
      expiresIn: session.expiresIn,
      loginTime: session.loginTime,
      source: session.source,
      monitorAccount: session.monitorAccount,
    } satisfies StoredSession);
    await transactionDone(transaction);
  } finally {
    db.close();
  }
}

export async function loadUserSession(): Promise<UserSession | null> {
  const db = await openDatabase();
  try {
    const transaction = db.transaction('state', 'readonly');
    const store = transaction.objectStore('state');
    const [storedKey, storedSession] = await Promise.all([
      requestAsPromise(store.get(SESSION_KEY_ID) as IDBRequest<StoredKey | undefined>),
      requestAsPromise(store.get(SESSION_ID) as IDBRequest<StoredSession | undefined>),
    ]);
    await transactionDone(transaction);
    if (!storedKey?.key || !storedSession) return null;
    const plain = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: bytes(storedSession.iv) },
      storedKey.key,
      storedSession.cipher,
    );
    const session: UserSession = {
      sno: storedSession.sno,
      accessToken: new TextDecoder().decode(plain),
      expiresIn: storedSession.expiresIn,
      loginTime: storedSession.loginTime,
      source: storedSession.source,
      monitorAccount: storedSession.monitorAccount === true,
    };
    if (session.expiresIn > 0 && session.loginTime + session.expiresIn <= Date.now() / 1000) {
      await clearUserSession();
      return null;
    }
    return session;
  } catch {
    await clearUserSession().catch(() => undefined);
    return null;
  } finally {
    db.close();
  }
}

export async function clearUserSession(): Promise<void> {
  const db = await openDatabase();
  try {
    const transaction = db.transaction(['state', 'cache'], 'readwrite');
    transaction.objectStore('state').clear();
    transaction.objectStore('cache').clear();
    await transactionDone(transaction);
  } finally {
    db.close();
  }
}

export async function cacheGet<T>(key: string): Promise<T | null> {
  const db = await openDatabase();
  try {
    const transaction = db.transaction('cache', 'readonly');
    const record = await requestAsPromise(
      transaction.objectStore('cache').get(key) as IDBRequest<CachedValue<T> | undefined>,
    );
    await transactionDone(transaction);
    if (!record || record.expiresAt <= Date.now()) return null;
    return record.value;
  } finally {
    db.close();
  }
}

export async function cacheSet<T>(key: string, value: T, ttlMs: number): Promise<void> {
  const db = await openDatabase();
  try {
    const transaction = db.transaction('cache', 'readwrite');
    transaction.objectStore('cache').put({
      key,
      value,
      expiresAt: Date.now() + ttlMs,
    } satisfies CachedValue<T>);
    await transactionDone(transaction);
  } finally {
    db.close();
  }
}
