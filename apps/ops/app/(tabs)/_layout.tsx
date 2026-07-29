import { Tabs } from 'expo-router';
import { Text } from 'react-native';
import {
  BuildingIcon, ClipboardIcon, HomeIcon, RupeeIcon, WrenchIcon,
  color, font, shadow,
} from '@dwellm8/mobile-shared';

const tab = (Icon: any) => ({ focused }: { focused: boolean }) => (
  <Icon size={25} c={focused ? color.accent : color.inkFaint} w={focused ? 2.1 : 1.8} />
);
const label = (text: string) => ({ focused }: { focused: boolean }) => (
  <Text style={{ ...font.small, fontSize: 11.5, color: focused ? color.accent : color.inkSoft, marginTop: 2 }}>
    {text}
  </Text>
);

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarStyle: {
          backgroundColor: '#FFFFFF', borderTopColor: color.line,
          height: 84, paddingTop: 8, ...shadow.bar,
        },
      }}
    >
      <Tabs.Screen name="index" options={{ tabBarIcon: tab(HomeIcon), tabBarLabel: label('Today') }} />
      <Tabs.Screen name="collect" options={{ tabBarIcon: tab(RupeeIcon), tabBarLabel: label('Collect') }} />
      <Tabs.Screen name="jobs" options={{ tabBarIcon: tab(WrenchIcon), tabBarLabel: label('Jobs') }} />
      <Tabs.Screen name="inspect" options={{ tabBarIcon: tab(ClipboardIcon), tabBarLabel: label('Inspect') }} />
      <Tabs.Screen name="portfolio" options={{ tabBarIcon: tab(BuildingIcon), tabBarLabel: label('Portfolio') }} />
    </Tabs>
  );
}
