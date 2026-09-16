package com.schat.app.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import com.schat.app.ui.screens.ChatScreen
import com.schat.app.ui.screens.ConversationListScreen
import com.schat.app.ui.screens.LoginScreen
import com.schat.app.ui.screens.NewChatScreen
import com.schat.app.ui.screens.ProfileScreen

object Routes {
    const val LOADING = "loading"
    const val LOGIN = "login"
    const val CONVERSATIONS = "conversations"
    const val CHAT = "chat/{convId}/{convName}"
    const val NEW_CHAT = "new_chat"
    const val PROFILE = "profile"

    fun chatRoute(convId: Long, convName: String) = "chat/$convId/${android.net.Uri.encode(convName)}"
}

@Composable
fun AppNavigation(activity: android.content.Context) {
    val vm: AppViewModel = viewModel()
    val navController = rememberNavController()

    val isInitializing by vm.isInitializing.collectAsState()
    val isLoggedIn by vm.isLoggedIn.collectAsState()

    // 初始化期间显示 Loading，完成后根据登录态决定起始页
    val start = when {
        isInitializing -> Routes.LOADING
        isLoggedIn -> Routes.CONVERSATIONS
        else -> Routes.LOGIN
    }

    NavHost(navController = navController, startDestination = start) {

        composable(Routes.LOADING) {
            Box(
                modifier = Modifier.fillMaxSize(),
                contentAlignment = Alignment.Center
            ) {
                CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
            }
            LaunchedEffect(isInitializing, isLoggedIn) {
                if (!isInitializing) {
                    if (isLoggedIn) {
                        navController.navigate(Routes.CONVERSATIONS) {
                            popUpTo(Routes.LOADING) { inclusive = true }
                        }
                    } else {
                        navController.navigate(Routes.LOGIN) {
                            popUpTo(Routes.LOADING) { inclusive = true }
                        }
                    }
                }
            }
        }

        composable(Routes.LOGIN) {
            LoginScreen(
                vm = vm,
                onSuccess = {
                    navController.navigate(Routes.CONVERSATIONS) {
                        popUpTo(Routes.LOGIN) { inclusive = true }
                    }
                }
            )
        }

        composable(Routes.CONVERSATIONS) {
            ConversationListScreen(
                vm = vm,
                onOpenConversation = { convId, convName ->
                    navController.navigate(Routes.chatRoute(convId, convName))
                },
                onNewChat = { navController.navigate(Routes.NEW_CHAT) },
                onProfile = { navController.navigate(Routes.PROFILE) }
            )
        }

        composable(
            route = Routes.CHAT,
            arguments = listOf(
                navArgument("convId") { type = NavType.LongType },
                navArgument("convName") { type = NavType.StringType }
            )
        ) { entry ->
            val convId = entry.arguments?.getLong("convId") ?: 0L
            val convName = entry.arguments?.getString("convName") ?: ""
            // 进入聊天页时打开会话
            LaunchedEffect(convId) {
                if (convId > 0) vm.openConversation(convId, convName)
            }
            ChatScreen(
                vm = vm,
                convId = convId,
                convName = convName,
                onBack = {
                    vm.closeConversation()
                    navController.popBackStack()
                }
            )
        }

        composable(Routes.NEW_CHAT) {
            NewChatScreen(
                vm = vm,
                onBack = { navController.popBackStack() },
                onConversationCreated = { convId, convName ->
                    navController.popBackStack()
                    navController.navigate(Routes.chatRoute(convId, convName))
                }
            )
        }

        composable(Routes.PROFILE) {
            ProfileScreen(
                vm = vm,
                onBack = { navController.popBackStack() }
            )
        }
    }
}
