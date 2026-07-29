import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Card, CloseIcon, HouseArt, MoneyRow, color, font, inr, radius, space } from '@rentora/mobile-shared';
import { currentInvoice, totalDue } from '../src/data/mock';

export default function PayConfirm() {
  const router = useRouter();
  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']} style={{ backgroundColor: '#FFF' }}>
        <View style={{ padding: space(4) }}>
          <Pressable onPress={() => router.back()} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ padding: space(4), alignItems: 'center' }}>
        <HouseArt size={190} />
        <Text style={s.title}>Opening your UPI app</Text>
        <Text style={s.body}>
          Approve {inr(totalDue)} in your UPI app and come straight back. Your receipt appears here
          the moment the payment confirms — we never mark it paid before then.
        </Text>

        <Card style={{ alignSelf: 'stretch', marginTop: space(5) }}>
          <MoneyRow label="Invoice" value={currentInvoice.number} />
          <MoneyRow label="Period" value={currentInvoice.period} />
          <MoneyRow label="Amount" value={inr(totalDue)} strong last />
        </Card>

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
  done: {
    backgroundColor: color.accent, borderRadius: radius.pill, alignSelf: 'stretch',
    paddingVertical: space(4), alignItems: 'center', marginTop: space(6),
  },
  doneText: { ...font.h3, color: '#FFF' },
});
