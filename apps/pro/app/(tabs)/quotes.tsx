import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, ListRow, StatusPill, Button, Metric,
  PlusIcon, SectionTitle,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { quotes, parts } from '../../src/data/mock';

/** Quotes raised against jobs, and the parts price list they are built from. */

const tone: Record<string, Tone> = { Approved: 'green', 'Awaiting owner': 'amber', Declined: 'red' };

export default function Quotes() {
  const router = useRouter();
  const approved = quotes.filter((q) => q.state === 'Approved');
  const value = approved.reduce((a, q) => a + q.amountPaise, 0);

  return (
    <>
      <AppHeader title="Quotes" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <View style={s.metrics}>
          <Metric value={String(quotes.length)} label="quotes raised" tone="blue" />
          <Metric value={String(approved.length)} label="approved" tone="green" />
          <Metric value={inr(value, { noPaise: true })} label="approved value" tone="violet" />
        </View>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {quotes.map((q, i) => (
            <ListRow
              key={q.id}
              title={q.job}
              subtitle={`${q.id.toUpperCase()} · raised ${q.at}`}
              meta={inr(q.amountPaise)}
              right={<StatusPill text={q.state} tone={tone[q.state] ?? 'neutral'} />}
              onPress={() => router.push(`/quote?id=${q.id}`)}
              last={i === quotes.length - 1}
            />
          ))}
        </Card>

        <Button
          label="Raise a quote"
          icon={<PlusIcon size={19} c="#FFF" />}
          onPress={() => router.push('/quote?id=new')}
          style={{ marginHorizontal: space(4), marginBottom: space(4) }}
        />

        <SectionTitle>Your rate card</SectionTitle>
        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {parts.map((p, i) => (
            <ListRow
              key={p.id}
              title={p.name}
              right={<Text style={s.price}>{inr(p.pricePaise)}</Text>}
              last={i === parts.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>How a quote is approved</Text>
          <Text style={s.body}>
            Anything within the manager's spend authority is approved on the spot. Above it, the owner
            decides and you will see the change here — never start on an unapproved quote, because
            unapproved work cannot be settled.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  price: { ...font.body, color: color.inkStrong, fontWeight: '700' },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
});
