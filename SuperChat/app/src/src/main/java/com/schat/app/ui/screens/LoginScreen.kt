package com.schat.app.ui.screens

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Bolt
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.schat.app.ui.AppViewModel
import com.schat.app.ui.theme.*
import kotlinx.coroutines.launch

@Composable
fun LoginScreen(vm: AppViewModel, onSuccess: () -> Unit) {
    val isLoading by vm.isLoading.collectAsState()
    val isLoggedIn by vm.isLoggedIn.collectAsState()
    val scope = rememberCoroutineScope()

    var isLogin by remember { mutableStateOf(true) }
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var displayName by remember { mutableStateOf("") }
    var errorMsg by remember { mutableStateOf("") }
    var hasNavigated by remember { mutableStateOf(false) }

    // 登录成功后导航（只导航一次）
    LaunchedEffect(isLoggedIn) {
        if (isLoggedIn && !hasNavigated) {
            hasNavigated = true
            onSuccess()
        }
    }

    // 收集错误事件
    LaunchedEffect(Unit) {
        vm.error.collect { errorMsg = it }
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Brush.verticalGradient(listOf(BluePrimary, PurpleAccent)))
            .padding(24.dp)
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState()),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center
        ) {
            Icon(
                imageVector = Icons.Filled.Bolt,
                contentDescription = "SuperChat",
                tint = TextWhite,
                modifier = Modifier.size(72.dp)
            )
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = "SuperChat",
                style = MaterialTheme.typography.headlineMedium,
                color = TextWhite,
                fontWeight = FontWeight.Bold
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = if (isLogin) "欢迎回来" else "创建新账号",
                color = TextWhite.copy(alpha = 0.8f),
                fontSize = 14.sp
            )

            Spacer(modifier = Modifier.height(40.dp))

            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(20.dp),
                colors = CardDefaults.cardColors(containerColor = SurfaceWhite)
            ) {
                Column(
                    modifier = Modifier.padding(24.dp),
                    horizontalAlignment = Alignment.CenterHorizontally
                ) {
                    OutlinedTextField(
                        value = username,
                        onValueChange = { username = it; errorMsg = "" },
                        label = { Text("用户名") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(modifier = Modifier.height(12.dp))
                    OutlinedTextField(
                        value = password,
                        onValueChange = { password = it; errorMsg = "" },
                        label = { Text("密码") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        modifier = Modifier.fillMaxWidth()
                    )
                    AnimatedVisibility(visible = !isLogin) {
                        Column {
                            Spacer(modifier = Modifier.height(12.dp))
                            OutlinedTextField(
                                value = displayName,
                                onValueChange = { displayName = it },
                                label = { Text("昵称（可选）") },
                                singleLine = true,
                                modifier = Modifier.fillMaxWidth()
                            )
                        }
                    }

                    if (errorMsg.isNotEmpty()) {
                        Spacer(modifier = Modifier.height(8.dp))
                        Text(text = errorMsg, color = UnreadRed, fontSize = 13.sp)
                    }

                    Spacer(modifier = Modifier.height(20.dp))

                    Button(
                        onClick = {
                            errorMsg = ""
                            if (username.isBlank() || password.isBlank()) {
                                errorMsg = "用户名和密码不能为空"
                                return@Button
                            }
                            if (!isLogin && password.length < 6) {
                                errorMsg = "密码至少 6 位"
                                return@Button
                            }
                            if (isLogin) {
                                vm.login(username.trim(), password)
                            } else {
                                vm.register(username.trim(), password, displayName.trim())
                            }
                        },
                        enabled = !isLoading,
                        modifier = Modifier.fillMaxWidth().height(52.dp),
                        shape = RoundedCornerShape(16.dp),
                        colors = ButtonDefaults.buttonColors(containerColor = BluePrimary)
                    ) {
                        if (isLoading) {
                            CircularProgressIndicator(
                                color = TextWhite, strokeWidth = 2.dp,
                                modifier = Modifier.size(24.dp)
                            )
                        } else {
                            Text(
                                text = if (isLogin) "登录" else "注册",
                                fontSize = 16.sp, fontWeight = FontWeight.Bold
                            )
                        }
                    }

                    Spacer(modifier = Modifier.height(12.dp))

                    TextButton(onClick = {
                        errorMsg = ""
                        isLogin = !isLogin
                    }) {
                        Text(
                            text = if (isLogin) "没有账号？去注册" else "已有账号？去登录",
                            color = TextSecondary, fontSize = 14.sp
                        )
                    }
                }
            }
        }
    }
}
