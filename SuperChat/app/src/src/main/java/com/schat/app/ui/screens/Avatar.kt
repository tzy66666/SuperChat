package com.schat.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil.compose.AsyncImage
import com.schat.app.network.ApiClient
import com.schat.app.ui.theme.BluePrimary
import com.schat.app.ui.theme.PurpleAccent
import com.schat.app.ui.theme.TextWhite

/**
 * 头像组件：有 avatarUrl 显示图片，没有则显示名字首字
 */
@Composable
fun Avatar(
    name: String,
    avatarUrl: String,
    isGroup: Boolean = false,
    size: Int = 52
) {
    val url = if (avatarUrl.isNotBlank()) ApiClient.avatarUrl(avatarUrl) else ""

    if (url.isNotBlank()) {
        AsyncImage(
            model = url,
            contentDescription = "头像",
            modifier = Modifier
                .size(size.dp)
                .clip(CircleShape)
        )
    } else {
        Box(
            modifier = Modifier
                .size(size.dp)
                .clip(CircleShape)
                .background(if (isGroup) PurpleAccent else BluePrimary),
            contentAlignment = Alignment.Center
        ) {
            Text(
                text = name.take(1).ifEmpty { "U" },
                color = TextWhite,
                fontWeight = FontWeight.Bold,
                fontSize = (size * 0.38).sp
            )
        }
    }
}
