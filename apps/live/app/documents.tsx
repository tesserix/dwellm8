import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Card, ChevronRight, CloseIcon, DocIcon, DottedRule, MoneyRow, color, font, inr, space } from '@dwellm8/mobile-shared';
import { documents, tenancy } from '../src/data/mock';

export default function Documents() {
  const router = useRouter();
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
          {documents.map((d, i) => (
            <View key={d.id}>
              <Pressable style={s.doc}>
                <DocIcon size={22} c={color.inkFaint} />
                <View style={{ flex: 1 }}>
                  <Text style={s.docName}>{d.name}</Text>
                  <Text style={s.docDate}>{d.date}</Text>
                </View>
                <ChevronRight size={20} c={color.inkFaint} />
              </Pressable>
              {i < documents.length - 1 ? <DottedRule /> : null}
            </View>
          ))}
        </Card>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  title: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  heading: { ...font.h2, color: color.inkStrong, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  doc: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: space(4) },
  docName: { ...font.body, color: color.inkStrong, fontWeight: '600' },
  docDate: { ...font.small, color: color.inkSoft, marginTop: 3 },
});
