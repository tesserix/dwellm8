import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, ListRow, Button, StatusPill, DocIcon,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { inr, taxPack } from '../src/data/mock';

/**
 * The tax pack.
 *
 * Once a year an owner needs house-property income in the shape the return
 * asks for. The arithmetic is shown rather than asserted, because an owner who
 * cannot follow it will not trust it — and their accountant certainly will not.
 */

export default function Tax() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const t = taxPack;

  return (
    <>
      <BackHeader title={`Tax pack ${t.year}`} subtitle="Income from house property" onBack={goBack} />
      <Screen>
        <Card>
          <StatusPill text="Ready to file" tone="green" dot />
          <Text style={s.big}>{inr(t.netIncomePaise, { noPaise: true })}</Text>
          <Text style={s.sub}>net income from house property, {t.year}</Text>

          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Rent received" v={inr(t.rentReceivedPaise)} tone="green" />
            <KeyValue k="Less municipal taxes paid" v={`− ${inr(t.municipalTaxPaise)}`} tone="red" />
            <KeyValue k="Annual value" v={inr(t.rentReceivedPaise - t.municipalTaxPaise)} />
            <KeyValue k="Less standard deduction (30%)" v={`− ${inr(t.standardDeductionPaise)}`} tone="red" />
            <KeyValue k="Less interest on borrowed capital" v={t.interestPaise ? `− ${inr(t.interestPaise)}` : '—'} />
            <KeyValue k="Net income" v={inr(t.netIncomePaise)} last />
          </View>
          <Text style={s.note}>
            Section 24 allows a flat 30% deduction on annual value whether or not you spent it —
            repairs are already inside that figure, which is why they are not deducted again here.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>TDS your tenant deducted</Text>
          <KeyValue k="Deducted and deposited" v={inr(t.tdsCreditPaise)} tone="green" />
          <KeyValue k="Available as credit" v="Yes — matches your 26AS" />
          <KeyValue k="Certificate" v="Form 16C, issued 30 Apr 2026" last />
          <Text style={s.note}>
            A tenant paying over ₹50,000 a month deducts TDS under section 194-IB. It is your money,
            already with the government — claim it as a credit, do not pay it twice.
          </Text>
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {t.documents.map((d, i) => (
            <ListRow
              key={d.id}
              left={<DocIcon size={20} c={color.inkFaint} />}
              title={d.name}
              subtitle={d.date}
              onPress={() => router.push('/documents')}
              last={i === t.documents.length - 1}
            />
          ))}
        </Card>

        <Button label="Email the pack to my accountant" onPress={() => {}} style={{ marginHorizontal: space(4) }} />
        <Text style={s.foot}>
          Figures are prepared from your ledger and are not tax advice. Your accountant should check
          them against your other income before you file.
        </Text>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  big: { fontSize: 34, fontWeight: '800', color: color.inkStrong, marginTop: space(3), letterSpacing: -0.6 },
  sub: { ...font.body, color: color.inkSoft, marginTop: 4 },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  foot: { ...font.small, color: color.inkFaint, textAlign: 'center', marginTop: space(4), paddingHorizontal: space(6), lineHeight: 18 },
});
