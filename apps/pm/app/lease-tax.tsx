import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, StatusPill, Button, ActionBar, KeyValue, ChoiceRow,
  Toast, color, font, space,
  deductorOptions, residencyOptions, pathFor, section195Acknowledgement, facilitationNotice,
  type DeductorClass, type Residency, useBack,
} from '@dwellm8/mobile-shared';

/**
 * The two facts a tenancy cannot start without: what kind of payer the tenant is,
 * and whether the landlord is a resident. ADR-0024.
 *
 * Asked here rather than at the first payout, because by then the rent has been
 * paid for months, the deduction was due monthly, and the interest runs from each
 * due date. The API refuses to activate a lease without these — this screen is
 * where they get answered rather than guessed.
 *
 * Both questions are asked in the tenant's own terms and the statutory class is
 * derived. "Are you liable to audit under section 44AB" is answerable by an
 * accountant and by nobody else at a lease signing.
 */

export default function LeaseTax() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const [deductor, setDeductor] = useState<DeductorClass | null>(null);
  const [residency, setResidency] = useState<Residency | null>(null);
  const [accepted, setAccepted] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const path = deductor && residency ? pathFor(deductor, residency) : null;
  const blocked = !!path?.needsAcknowledgement && !accepted;
  const ready = !!path && !blocked;

  return (
    <>
      <BackHeader
        title="Tax on the rent"
        subtitle="Two questions, before the tenancy starts"
        onBack={goBack}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <Text style={s.h}>Who pays the rent?</Text>
          <Text style={s.lede}>
            The tenant deducts the tax, so the answer is about them and not about the landlord.
          </Text>
          {deductorOptions.map((o, i) => (
            <ChoiceRow
              key={o.k}
              label={o.label}
              hint={o.hint}
              selected={deductor === o.k}
              onPress={() => setDeductor(o.k)}
              last={i === deductorOptions.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>Is the landlord resident in India?</Text>
          <Text style={s.lede}>
            Residence for tax is a day count, not an address or a passport. If the landlord is
            unsure, their accountant will know — and an answer given here is recorded as a
            declaration with a date against it.
          </Text>
          {residencyOptions.map((o, i) => (
            <ChoiceRow
              key={o.k}
              label={o.label}
              hint={o.hint}
              selected={residency === o.k}
              onPress={() => {
                setResidency(o.k);
                if (o.k === 'resident') setAccepted(false);
              }}
              last={i === residencyOptions.length - 1}
            />
          ))}
        </Card>

        {path ? (
          <Card>
            <View style={s.row}>
              <StatusPill
                text={`Section ${path.name}`}
                tone={path.section === '195' ? 'amber' : 'blue'}
                dot
              />
            </View>
            <Text style={s.title}>What this means for the tenant</Text>
            <View style={{ marginTop: space(2) }}>
              <KeyValue k="When tax is deducted" v={path.when} />
              <KeyValue k="Threshold" v={path.threshold} />
              <KeyValue k="TAN needed" v={path.needsTAN ? 'Yes' : 'No — this is the section that avoids it'} />
              <KeyValue k="Forms" v={path.artefacts.join(', ')} last />
            </View>
            <Text style={s.note}>{facilitationNotice}</Text>
          </Card>
        ) : null}

        {path?.needsAcknowledgement ? (
          <Card>
            <StatusPill text="Acknowledgement required" tone="red" dot />
            <Text style={s.title}>The landlord is a non-resident</Text>
            <Text style={s.lede}>
              This is the case where an unaware tenant is most exposed, so the tenancy cannot start
              until they have read what follows and accepted it.
            </Text>
            <View style={{ marginTop: space(3) }}>
              {section195Acknowledgement.map((line, i) => (
                <View key={i} style={s.point}>
                  <Text style={s.bullet}>{i + 1}</Text>
                  <Text style={s.pointText}>{line}</Text>
                </View>
              ))}
            </View>
            <View style={{ marginTop: space(4) }}>
              <Button
                label={accepted ? 'Accepted' : 'The tenant accepts this obligation'}
                tone={accepted ? 'secondary' : 'primary'}
                onPress={() => {
                  setAccepted(true);
                  setToast('Acknowledgement recorded against today’s date');
                }}
              />
            </View>
            <Text style={s.note}>
              Recorded against these answers and this date. If the landlord’s residence changes
              later, the tenancy moves to a different section from that date and a fresh
              acknowledgement is asked for — the earlier months keep the section they were
              deducted under.
            </Text>
          </Card>
        ) : null}
      </Screen>

      <ActionBar>
        <Button
          label={
            !path
              ? 'Answer both questions'
              : blocked
                ? 'Acknowledgement outstanding'
                : 'Save and continue'
          }
          disabled={!ready}
          onPress={() => {
            setToast('Saved. The tenancy can now be activated.');
            router.back();
          }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.ink, marginBottom: space(1) },
  lede: { ...font.body, color: color.inkSoft, marginBottom: space(3), lineHeight: 20 },
  title: { ...font.h3, color: color.ink, marginTop: space(2) },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  note: { ...font.small, color: color.inkFaint, marginTop: space(3), lineHeight: 18 },
  point: { flexDirection: 'row', gap: 10, marginBottom: space(3) },
  bullet: {
    ...font.small, color: color.inkSoft, width: 20, height: 20, borderRadius: 10,
    textAlign: 'center', lineHeight: 20, backgroundColor: color.line,
  },
  pointText: { ...font.body, color: color.ink, flex: 1, lineHeight: 20 },
});
