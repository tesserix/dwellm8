import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { color, Shell } from '@dwellm8/mobile-shared';

export default function RootLayout() {
  return (
    <SafeAreaProvider>
      <StatusBar style="dark" />
      <Shell>
        <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: color.bgTop } }}>
          <Stack.Screen name="(tabs)" />
          <Stack.Screen name="job" options={{ presentation: 'modal' }} />
          <Stack.Screen name="otp" options={{ presentation: 'modal' }} />
          <Stack.Screen name="complete" options={{ presentation: 'modal' }} />
          <Stack.Screen name="quote" options={{ presentation: 'modal' }} />
          <Stack.Screen name="profile" />
        </Stack>
      </Shell>
    </SafeAreaProvider>
  );
}
