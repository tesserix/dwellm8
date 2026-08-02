import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Card, CloseIcon, MoneyRow, color, font, inr, space } from '@dwellm8/mobile-shared';
import { useLiveData } from '../src/data/source';

export default function Documents() {
  const router = useRouter();
  const { tenancy } = useLiveData();
  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']} style={{ backgroundColor: '#FFF' }}>
        <View style={{ padding: space(4) }}>
          <Pressable onPress={() => router.back()} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
          <Text style={s.title}>Your tenancy</Text>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ paddingVertical: space(4), paddingBottom: space(10) }}>
        <Card>
          <MoneyRow label="Monthly rent" value={inr(tenancy.rentPaise)} />
          <MoneyRow label="Deposit held" value={inr(tenancy.depositPaise)} />
          <MoneyRow label="Rent paid to" value={tenancy.paidTo} tone="positive" />
          <MoneyRow label="Lease expires" value={tenancy.leaseExpires} />
          <MoneyRow label="Notice period" value={`${tenancy.noticeDays} days`} last />
        </Card>

        <Text style={s.heading}>Documents</Text>
        <Card>
          <Text style={s.empty}>
            Your executed agreement and tenancy documents will appear here once
            document sharing reaches the tenant app. Receipts already live under
            Payments → Receipts.
          </Text>
        </Card>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  title: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  heading: { ...font.h2, color: color.inkStrong, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  empty: { ...font.body, color: color.inkSoft, lineHeight: 22 },
});
