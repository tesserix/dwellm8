import { Tabs } from 'expo-router';
import { Text, View } from 'react-native';
import {
  ChatIcon,
  HomeIcon,
  RupeeIcon,
  color,
  font,
  shadow,
} from '@rentora/mobile-shared';

const tab = (Icon: any) => ({ focused }: { focused: boolean }) => (
  <Icon size={26} c={focused ? color.accent : color.inkFaint} w={focused ? 2.1 : 1.8} />
);
const label = (text: string) => ({ focused }: { focused: boolean }) => (
  <Text style={{ ...font.small, color: focused ? color.accent : color.inkSoft, marginTop: 2 }}>{text}</Text>
);

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarStyle: {
          backgroundColor: '#FFFFFF',
          borderTopColor: color.line,
          height: 84,
          paddingTop: 8,
          ...shadow.bar,
        },
      }}
    >
      <Tabs.Screen name="index" options={{ tabBarIcon: tab(HomeIcon), tabBarLabel: label('Home') }} />
      <Tabs.Screen name="financials" options={{ tabBarIcon: tab(RupeeIcon), tabBarLabel: label('Financials') }} />
      <Tabs.Screen name="chat" options={{ tabBarIcon: tab(ChatIcon), tabBarLabel: label('Chat') }} />
    </Tabs>
  );
}
