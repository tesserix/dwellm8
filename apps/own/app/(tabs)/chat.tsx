import React from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import { threads } from '../../src/data/mock';
import {
  AppHeader,
  AvatarButton,
  Card,
  ChevronRight,
  DottedRule,
  Screen,
  color,
  font,
  radius,
  space,
} from '@dwellm8/mobile-shared';

export default function Chat() {
  const router = useRouter();
  return (
    <>
      <AppHeader
        title="Chat"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <Card style={{ marginTop: space(4) }}>
          {threads.map((t, i) => (
            <View key={t.id}>
              <Pressable style={s.row} onPress={() => router.push('/thread')}>
                <View style={s.logo}>
                  <Text style={s.logoText}>{t.agency.split(' ').map((w) => w[0]).join('').slice(0, 2)}</Text>
                </View>
                <View style={{ flex: 1 }}>
                  <Text style={s.agency}>{t.agency}</Text>
                  {t.preview ? <Text style={s.preview}>{t.preview}</Text> : null}
                </View>
                <ChevronRight size={22} c={color.inkFaint} />
              </Pressable>
              {i < threads.length - 1 ? <DottedRule /> : null}
            </View>
          ))}
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: 14, paddingVertical: space(4) },
  logo: {
    width: 58, height: 58, borderRadius: radius.md, backgroundColor: '#F3F6FA',
    alignItems: 'center', justifyContent: 'center',
  },
  logoText: { ...font.h3, color: color.accentDeep },
  agency: { ...font.h3, color: color.inkStrong },
  preview: { ...font.body, color: color.inkFaint, marginTop: 3 },
});
