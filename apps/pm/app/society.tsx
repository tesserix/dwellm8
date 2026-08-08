import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
 Avatar, BackHeader, Button, Card, color, Field, font, inr, inrShort, KeyValue, ListRow,
  Metric, MetricRow, PlusIcon, ProgressBar, Screen, Segmented, space, StatusPill, Toast,
  useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { amenities, society, societyDues, societyNotices } from '../src/data/mock';

/**
 * The society (RWA) surface.
 *
 * A committee runs on three things: whether the dues came in, whether people
 * were told, and who booked the clubhouse. Everything else can wait for the
 * AGM, so those three are the whole screen.
 */

const dueTone: Record<string, Tone> = { Paid: 'green', Due: 'amber', Late: 'red' };

export default function Society() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const [tab, setTab] = useState('Dues');
  const [notice, setNotice] = useState('');
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  return (
    <>
      <BackHeader title={society.name} subtitle={`${society.flats} flats · ${society.committee}`} onBack={goBack} />
      <Screen>
        <DemoNote issue={300} />
        {toast ? <Toast text={toast} /> : null}

        <MetricRow>
          <Metric value={`${society.collectedPct}%`} label="dues collected" tone={society.collectedPct > 85 ? 'green' : 'amber'} />
          <Metric value={String(society.defaulters)} label="flats in arrears" tone="red" />
          <Metric value={inrShort(society.corpusPaise)} label="corpus" tone="blue" />
        </MetricRow>

        <View style={{ marginBottom: space(3) }}>
          <Segmented items={['Dues', 'Notices', 'Amenities']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Dues' ? (
          <>
            <Card>
              <Text style={s.h}>August maintenance</Text>
              <Text style={s.sub}>{inr(society.monthlyDuePaise, { noPaise: true })} per flat · raised on the 1st</Text>
              <ProgressBar pct={society.collectedPct} tint={color.positive} />
              <View style={{ marginTop: space(4) }}>
                <KeyValue k="Billed" v={inr(society.monthlyDuePaise * society.flats)} />
                <KeyValue k="Outstanding" v={inr(society.arrearsPaise)} tone="red" last />
              </View>
            </Card>

            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              {societyDues.map((d, i) => (
                <ListRow
                  key={d.id}
                  left={<Avatar initials={d.flat.replace('-', '').slice(0, 2)} tone={dueTone[d.state]} />}
                  title={`${d.flat} — ${d.resident}`}
                  subtitle={d.state === 'Paid' ? 'Paid in full' : `${inr(d.duePaise, { noPaise: true })} over ${d.months} month${d.months > 1 ? 's' : ''}`}
                  right={<StatusPill text={d.state} tone={dueTone[d.state]} />}
                  onPress={() => d.state !== 'Paid' && say(`Reminder sent to ${d.flat}`)}
                  last={i === societyDues.length - 1}
                  tone={d.state === 'Late' ? 'red' : undefined}
                />
              ))}
            </Card>
            <Text style={s.note}>
              Society dues post to the same ledger as rent. A resident who pays both sees one
              receipt, and the society's books stay separate from the owner's.
            </Text>
          </>
        ) : null}

        {tab === 'Notices' ? (
          <>
            <Card>
              <Text style={s.h}>Post a notice</Text>
              <Field
                label="What do residents need to know?"
                value={notice}
                onChange={setNotice}
                placeholder="Lift B out of service Tuesday, 10:00 – 16:00, for the annual inspection."
                multiline
              />
              <Button
                label="Send to 120 flats"
                icon={<PlusIcon size={18} c="#FFF" />}
                onPress={() => { setNotice(''); say('Notice sent — app and WhatsApp'); }}
                disabled={!notice}
              />
            </Card>

            {societyNotices.map((n) => (
              <Card key={n.id}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                  <StatusPill text={n.audience} tone="blue" />
                  <View style={{ flex: 1 }} />
                  <Text style={s.at}>{n.at}</Text>
                </View>
                <Text style={s.title}>{n.title}</Text>
                <Text style={s.body}>{n.body}</Text>
              </Card>
            ))}
          </>
        ) : null}

        {tab === 'Amenities' ? (
          <Card padded={false} style={{ paddingHorizontal: space(4) }}>
            {amenities.map((a, i) => (
              <ListRow
                key={a.id}
                title={a.name}
                subtitle={a.next}
                meta={a.ratePaise ? `${inr(a.ratePaise, { noPaise: true })} per booking` : 'Free to residents'}
                right={a.bookings ? <StatusPill text={`${a.bookings} booked`} tone="violet" /> : undefined}
                onPress={() => say(`${a.name} — booking calendar`)}
                last={i === amenities.length - 1}
              />
            ))}
          </Card>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 4, marginBottom: space(2) },
  title: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  body: { ...font.body, color: color.inkSoft, marginTop: 6, lineHeight: 21 },
  at: { ...font.small, color: color.inkFaint },
  note: { ...font.small, color: color.inkSoft, marginHorizontal: space(4), marginTop: space(2), lineHeight: 18 },
});

/** This screen has no endpoint behind it yet — say so rather than let a
 * manager act on figures that are not theirs. */
const DemoNote = ({ issue }: { issue: number }) => (
  <Text style={sDemo.note}>
    Demonstration data — this screen has no API behind it yet (#{issue}).
  </Text>
);

const sDemo = StyleSheet.create({
  note: { ...font.small, color: color.inkFaint, marginHorizontal: space(4), marginTop: space(3) },
});
