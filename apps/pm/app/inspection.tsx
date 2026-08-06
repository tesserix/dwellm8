import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, TriState, PhotoStrip, Button, ActionBar, KeyValue,
  StatusPill, Toast, Field, ProgressBar, CollapsibleHeader, SyncBadge,
  color, font, space,
} from '@dwellm8/mobile-shared';
import { inspectionRooms, inspections } from '../src/data/mock';

/**
 * Inspection capture.
 *
 * Designed for a corridor with one bar of signal: every mark is local, the
 * progress bar is honest about what is left, and submission queues rather
 * than claiming success it cannot prove.
 */

type Mark = 'ok' | 'note' | 'issue';

export default function InspectionScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const insp = inspections.find((x) => x.id === id) ?? inspections[0];

  const [marks, setMarks] = useState<Record<string, Mark>>({});
  const [open, setOpen] = useState<string | null>('r1');
  const [summary, setSummary] = useState('');
  const [submitted, setSubmitted] = useState(false);

  const allItems = inspectionRooms.flatMap((r) => r.items.map((it) => `${r.id}:${it}`));
  const doneCount = allItems.filter((k) => marks[k]).length;
  const pct = Math.round((doneCount / allItems.length) * 100);
  const issues = Object.values(marks).filter((m) => m === 'issue').length;

  if (submitted) {
    return (
      <>
        <BackHeader title="Report queued" onBack={() => router.back()} />
        <Screen>
        <DemoNote issue={298} />
          <Toast text="Saved on the phone — uploading when you have signal" />
          <Card>
            <StatusPill text="Queued to sync" tone="amber" />
            <Text style={s.h2}>{insp.kind} inspection — {insp.unit}</Text>
            <KeyValue k="Items checked" v={`${doneCount} of ${allItems.length}`} />
            <KeyValue k="Issues found" v={String(issues)} tone={issues ? 'red' : 'green'} />
            <KeyValue k="Photos" v="6" />
            <KeyValue k="Notice served" v={insp.noticeServed} last />
            <Text style={s.note}>
              Two jobs will be raised from the issues once the report uploads. The owner gets the
              report; the tenant gets the parts that concern them.
            </Text>
          </Card>
          <Button label="Back to inspections" onPress={() => router.back()} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader
        title={`${insp.kind} inspection`}
        subtitle={`${insp.unit} · ${insp.window}`}
        onBack={() => router.back()}
        right={<StatusPill text={`${pct}%`} tone={pct === 100 ? 'green' : 'amber'} />}
      />
      <Screen>
        <DemoNote issue={298} />
        <SyncBadge queued={doneCount} />

        <Card>
          <View style={{ flexDirection: 'row', justifyContent: 'space-between' }}>
            <Text style={s.label}>Progress</Text>
            <Text style={s.label}>{doneCount} of {allItems.length} items</Text>
          </View>
          <ProgressBar pct={pct} tint={pct === 100 ? color.positive : color.accent} />
          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Tenant" v={insp.tenant} />
            <KeyValue k="Notice served" v={insp.noticeServed} />
            <KeyValue k="Issues so far" v={String(issues)} tone={issues ? 'red' : 'green'} last />
          </View>
        </Card>

        {inspectionRooms.map((room) => {
          const isOpen = open === room.id;
          const roomDone = room.items.filter((it) => marks[`${room.id}:${it}`]).length;
          return (
            <View key={room.id}>
              <CollapsibleHeader
                title={`${room.name}  ·  ${roomDone}/${room.items.length}`}
                open={isOpen}
                onToggle={() => setOpen(isOpen ? null : room.id)}
              />
              {isOpen ? (
                <Card>
                  {room.items.map((it) => {
                    const key = `${room.id}:${it}`;
                    return (
                      <TriState
                        key={key}
                        label={it}
                        value={marks[key] ?? null}
                        onChange={(v) => setMarks((m) => ({ ...m, [key]: v }))}
                      />
                    );
                  })}
                  <PhotoStrip count={roomDone > 2 ? 2 : 0} onAdd={() => {}} />
                </Card>
              ) : null}
            </View>
          );
        })}

        <Card>
          <Text style={s.h3}>Summary for the owner</Text>
          <Field
            label="Plain language — this goes into the report"
            value={summary}
            onChange={setSummary}
            placeholder="Flat is well kept. Two issues: bathroom seepage and a loose balcony railing."
            multiline
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Save draft" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button label="Submit report" onPress={() => setSubmitted(true)} disabled={doneCount === 0} style={{ flex: 2 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  label: { ...font.small, color: color.inkSoft },
  h2: { ...font.h3, color: color.inkStrong, marginVertical: space(3) },
  h3: { ...font.h3, color: color.inkStrong, marginBottom: space(3) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
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
