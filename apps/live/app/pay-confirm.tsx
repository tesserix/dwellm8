import React, { useEffect } from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView, Linking } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Card, CloseIcon, HouseArt, MoneyRow, color, font, inr, radius, space } from '@dwellm8/mobile-shared';

export default function PayConfirm() {
  const router = useRouter();
  const { status, amount, url } = useLocalSearchParams<{ status?: string; amount?: string; url?: string }>();
  const paise = Number(amount ?? 0);
  const offline = status === 'recorded' || status === 'pending_confirmation';

  // The UPI intent opens as soon as the screen does; the button below is the
  // retry for when the OS swallowed the first attempt.
  useEffect(() => {
    if (url) Linking.openURL(url).catch(() => {});
  }, [url]);

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']} style={{ backgroundColor: '#FFF' }}>
        <View style={{ padding: space(4) }}>
          <Pressable onPress={() => router.back()} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ padding: space(4), alignItems: 'center' }}>
        <HouseArt size={190} />
        <Text style={s.title}>{offline ? 'Payment recorded' : 'Opening your UPI app'}</Text>
        <Text style={s.body}>
          {offline
            ? 'Your landlord will confirm the payment, and your receipt appears the moment they do.'
            : `Approve ${inr(paise)} in your UPI app and come straight back. Your receipt appears here the moment the payment confirms — we never mark it paid before then.`}
        </Text>

        <Card style={{ alignSelf: 'stretch', marginTop: space(5) }}>
          {status ? <MoneyRow label="Status" value={status.replaceAll('_', ' ')} /> : null}
          <MoneyRow label="Amount" value={inr(paise)} strong last />
        </Card>

        {url ? (
          <Pressable style={s.open} onPress={() => Linking.openURL(url).catch(() => {})}>
            <Text style={s.openText}>Open UPI app</Text>
          </Pressable>
        ) : null}

        <Pressable style={s.done} onPress={() => router.back()}>
          <Text style={s.doneText}>Done</Text>
        </Pressable>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  title: { ...font.h1, color: color.inkStrong, marginTop: space(4), textAlign: 'center' },
  body: { ...font.body, color: color.inkSoft, textAlign: 'center', marginTop: space(3), lineHeight: 23 },
  open: {
    borderWidth: 1.6, borderColor: color.accent, borderRadius: radius.pill, alignSelf: 'stretch',
    paddingVertical: space(4), alignItems: 'center', marginTop: space(5),
  },
  openText: { ...font.h3, color: color.accent },
  done: {
    backgroundColor: color.accent, borderRadius: radius.pill, alignSelf: 'stretch',
    paddingVertical: space(4), alignItems: 'center', marginTop: space(3),
  },
  doneText: { ...font.h3, color: '#FFF' },
});
