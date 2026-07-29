import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Metric, ListRow, StatusPill, KeyValue,
  SectionTitle,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { earnings, tech } from '../../src/data/mock';

/**
 * Earnings.
 *
 * A technician's most common question is "when am I paid, and why is it less
 * than the quote". The settlement date and the TDS line are therefore on the
 * screen, not buried in a statement.
 */

export default function Earnings() {
  const router = useRouter();

  return (
    <>
      <AppHeader title="Earnings" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <Card>
          <Text style={s.label}>Earned this month</Text>
          <Text style={s.big}>{inr(earnings.monthPaise, { noPaise: true })}</Text>
          <Text style={s.sub}>{earnings.jobsThisWeek} jobs this week · {tech.jobsDone} lifetime</Text>

          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Metric value={inr(earnings.weekPaise, { noPaise: true })} label="this week" tone="green" />
            <Metric value={inr(earnings.pendingPaise, { noPaise: true })} label="pending settlement" tone="amber" />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>Next settlement</Text>
          <KeyValue k="Date" v={earnings.nextSettlement} />
          <KeyValue k="Amount" v={inr(earnings.pendingPaise)} tone="green" />
          <KeyValue k="To" v="Sahyadri Facility Services · HDFC ••8821" />
          <KeyValue k="TDS" v="1% under section 194C, deducted at source" last />
          <Text style={s.note}>
            Dwellm8 settles to your firm, not to you directly. A job is only settled once the tenant
            or manager has signed it off, so complete the sign-off on site.
          </Text>
        </Card>

        <SectionTitle>Recent</SectionTitle>
        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {earnings.ledger.map((e, i) => (
            <ListRow
              key={e.id}
              title={e.label}
              subtitle={e.at}
              right={
                <View style={{ alignItems: 'flex-end', gap: 6 }}>
                  <Text style={[s.amt, e.paise < 0 && { color: color.negative }]}>
                    {inr(e.paise, { sign: true })}
                  </Text>
                  <StatusPill text={e.state} tone={e.state === 'Paid' ? 'green' : 'amber'} />
                </View>
              }
              last={i === earnings.ledger.length - 1}
            />
          ))}
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  label: { ...font.label, color: color.inkSoft },
  big: { fontSize: 34, fontWeight: '800', color: color.inkStrong, letterSpacing: -0.6, marginTop: 4 },
  sub: { ...font.small, color: color.inkSoft, marginTop: 6 },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  amt: { ...font.body, color: color.positive, fontWeight: '700' },
});
