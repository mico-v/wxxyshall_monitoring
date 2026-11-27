package com.micov.wxxyshall.ui

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ReceiptLong
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import androidx.navigation.navDeepLink
import androidx.fragment.app.FragmentActivity
import com.micov.wxxyshall.MainActivity
import com.micov.wxxyshall.ui.screens.AddRoomScreen
import com.micov.wxxyshall.ui.screens.DashboardScreen
import com.micov.wxxyshall.ui.screens.LoginScreen
import com.micov.wxxyshall.ui.screens.OrdersScreen
import com.micov.wxxyshall.ui.screens.RechargeScreen
import com.micov.wxxyshall.ui.screens.RoomDetailScreen
import com.micov.wxxyshall.ui.screens.SettingsScreen

private object Routes {
    const val DASHBOARD = "dashboard"
    const val ORDERS = "orders"
    const val SETTINGS = "settings"
    const val ROOM = "room?campus={campus}&building={building}&room={room}"
    const val EDIT_ROOM = "edit-room?key={key}"
    const val RECHARGE = "recharge?campus={campus}&building={building}&room={room}"

    fun room(campus: String, building: String, room: String): String =
        "room?campus=${android.net.Uri.encode(campus)}" +
            "&building=${android.net.Uri.encode(building)}" +
            "&room=${android.net.Uri.encode(room)}"

    fun editRoom(key: String? = null): String =
        "edit-room?key=${android.net.Uri.encode(key.orEmpty())}"

    fun recharge(campus: String, building: String, room: String): String =
        "recharge?campus=${android.net.Uri.encode(campus)}" +
            "&building=${android.net.Uri.encode(building)}" +
            "&room=${android.net.Uri.encode(room)}"
}

@Composable
fun WxxyshallRoot(activity: MainActivity) {
    val viewModel: AppViewModel = viewModel()
    val ready by viewModel.sessionReady.collectAsStateWithLifecycle()
    val session by viewModel.session.collectAsStateWithLifecycle()
    val message by viewModel.message.collectAsStateWithLifecycle()
    val warmDeepLink by activity.warmDeepLink.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val context = LocalContext.current
    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) {}

    LaunchedEffect(session) {
        if (session != null &&
            Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.POST_NOTIFICATIONS,
            ) != PackageManager.PERMISSION_GRANTED
        ) {
            permissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
    LaunchedEffect(message) {
        message?.let {
            snackbar.showSnackbar(it)
            viewModel.clearMessage()
        }
    }

    if (!ready) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            CircularProgressIndicator()
        }
        return
    }
    if (session == null) {
        LoginScreen(viewModel, snackbar)
        return
    }
    MainNavigation(
        activity = activity,
        viewModel = viewModel,
        snackbar = snackbar,
        warmDeepLink = warmDeepLink,
        onWarmDeepLinkConsumed = activity::consumeWarmDeepLink,
    )
}

@Composable
private fun MainNavigation(
    activity: FragmentActivity,
    viewModel: AppViewModel,
    snackbar: SnackbarHostState,
    warmDeepLink: android.net.Uri?,
    onWarmDeepLinkConsumed: () -> Unit,
) {
    val navController = rememberNavController()
    val backStack by navController.currentBackStackEntryAsState()
    val route = backStack?.destination?.route
    val showBottomBar = route == Routes.DASHBOARD || route == Routes.ORDERS || route == Routes.SETTINGS
    val biometric = remember(activity) { BiometricAuth(activity) }
    val targets by viewModel.targets.collectAsStateWithLifecycle()

    LaunchedEffect(warmDeepLink) {
        val uri = warmDeepLink ?: return@LaunchedEffect
        if (uri.scheme == "wxxyshall" && uri.host == "room") {
            val campus = uri.getQueryParameter("campus").orEmpty()
            val building = uri.getQueryParameter("building").orEmpty()
            val room = uri.getQueryParameter("room").orEmpty()
            if (campus.isNotBlank() && building.isNotBlank() && room.isNotBlank()) {
                navController.navigate(Routes.room(campus, building, room)) {
                    launchSingleTop = true
                }
            }
        }
        onWarmDeepLinkConsumed()
    }

    Scaffold(
        contentWindowInsets = WindowInsets(0, 0, 0, 0),
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = {
            if (showBottomBar) {
                NavigationBar {
                    listOf(
                        Triple(Routes.DASHBOARD, "总览", Icons.Outlined.Home),
                        Triple(Routes.ORDERS, "订单", Icons.AutoMirrored.Outlined.ReceiptLong),
                        Triple(Routes.SETTINGS, "设置", Icons.Outlined.Settings),
                    ).forEach { (destination, label, icon) ->
                        NavigationBarItem(
                            selected = route == destination,
                            onClick = {
                                navController.navigate(destination) {
                                    popUpTo(navController.graph.findStartDestination().id) {
                                        saveState = true
                                    }
                                    launchSingleTop = true
                                    restoreState = true
                                }
                            },
                            icon = { Icon(icon, contentDescription = label) },
                            label = { Text(label) },
                        )
                    }
                }
            }
        },
    ) { padding ->
        NavHost(
            navController = navController,
            startDestination = Routes.DASHBOARD,
            modifier = Modifier.padding(padding),
        ) {
            composable(Routes.DASHBOARD) {
                DashboardScreen(
                    viewModel = viewModel,
                    onRoom = { target ->
                        navController.navigate(Routes.room(target.campus, target.building, target.room))
                    },
                    onAdd = { navController.navigate(Routes.editRoom()) },
                    onSettings = { navController.navigate(Routes.SETTINGS) },
                )
            }
            composable(
                route = Routes.ROOM,
                arguments = listOf(
                    navArgument("campus") { type = NavType.StringType; defaultValue = "" },
                    navArgument("building") { type = NavType.StringType; defaultValue = "" },
                    navArgument("room") { type = NavType.StringType; defaultValue = "" },
                ),
                deepLinks = listOf(
                    navDeepLink {
                        uriPattern =
                            "wxxyshall://room?campus={campus}&building={building}&room={room}"
                    },
                ),
            ) { entry ->
                val campus = entry.arguments?.getString("campus").orEmpty()
                val building = entry.arguments?.getString("building").orEmpty()
                val room = entry.arguments?.getString("room").orEmpty()
                val target = targets.firstOrNull {
                    it.campus == campus && it.building == building && it.room == room
                }
                RoomDetailScreen(
                    viewModel = viewModel,
                    target = target,
                    onBack = { navController.popBackStack() },
                    onEdit = { target?.let { navController.navigate(Routes.editRoom(it.key)) } },
                    onRecharge = {
                        target?.let {
                            navController.navigate(
                                Routes.recharge(it.campus, it.building, it.room),
                            )
                        }
                    },
                )
            }
            composable(
                route = Routes.EDIT_ROOM,
                arguments = listOf(
                    navArgument("key") { type = NavType.StringType; defaultValue = "" },
                ),
            ) { entry ->
                val key = entry.arguments?.getString("key").orEmpty()
                val existing = targets.firstOrNull { it.key == key }
                AddRoomScreen(
                    viewModel = viewModel,
                    existing = existing,
                    onBack = { navController.popBackStack() },
                    onSaved = { navController.popBackStack() },
                )
            }
            composable(
                route = Routes.RECHARGE,
                arguments = listOf(
                    navArgument("campus") { type = NavType.StringType; defaultValue = "" },
                    navArgument("building") { type = NavType.StringType; defaultValue = "" },
                    navArgument("room") { type = NavType.StringType; defaultValue = "" },
                ),
            ) { entry ->
                val campus = entry.arguments?.getString("campus").orEmpty()
                val building = entry.arguments?.getString("building").orEmpty()
                val room = entry.arguments?.getString("room").orEmpty()
                val target = targets.firstOrNull {
                    it.campus == campus && it.building == building && it.room == room
                }
                RechargeScreen(
                    viewModel = viewModel,
                    target = target,
                    biometric = biometric,
                    onBack = { navController.popBackStack() },
                )
            }
            composable(Routes.ORDERS) {
                OrdersScreen(viewModel, onDashboard = {
                    navController.navigate(Routes.DASHBOARD) {
                        launchSingleTop = true
                    }
                })
            }
            composable(Routes.SETTINGS) {
                SettingsScreen(
                    viewModel = viewModel,
                    onEditTarget = { navController.navigate(Routes.editRoom(it.key)) },
                    onAddTarget = { navController.navigate(Routes.editRoom()) },
                )
            }
        }
    }
}
