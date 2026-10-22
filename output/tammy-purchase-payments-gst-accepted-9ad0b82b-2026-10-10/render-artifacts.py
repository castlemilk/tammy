import json
import math
import subprocess
import sys
from pathlib import Path

from PIL import Image, ImageDraw

base = Path(sys.argv[1]).resolve(strict=True)
audit = json.loads((base / 'independent-inspection.json').read_text())
visual = base / 'visual'
visual.mkdir(mode=0o700, exist_ok=False)
poppler = Path('/Users/benebsworth/.cache/codex-runtimes/codex-primary-runtime/dependencies/bin/override')
records = []
pdfs = sorted({entry['pdf'] for entry in audit['comparisons']})
for relative in pdfs:
    pdf = base / relative
    info = subprocess.run([str(poppler / 'pdfinfo'), str(pdf)], capture_output=True, text=True, check=True)
    pages = int(next(line.split(':', 1)[1].strip() for line in info.stdout.splitlines() if line.startswith('Pages:')))
    stem = relative.removesuffix('.pdf').replace('/', '--')
    selected = list(range(1, pages + 1)) if pages <= 8 else [1, math.ceil(pages / 2), pages]
    full = []
    for page in selected:
        target = visual / f'{stem}--page-{page:03}'
        subprocess.run([str(poppler / 'pdftoppm'), '-f', str(page), '-l', str(page), '-singlefile', '-r', '100', '-png', str(pdf), str(target)], capture_output=True, check=True)
        full.append(str(target.with_suffix('.png').relative_to(base)))
    thumbnails = visual / f'{stem}--thumb'
    subprocess.run([str(poppler / 'pdftoppm'), '-r', '36', '-png', str(pdf), str(thumbnails)], capture_output=True, check=True)
    files = sorted(visual.glob(f'{stem}--thumb-*.png'))
    assert len(files) == pages
    sheets = []
    for start in range(0, pages, 24):
        subset = files[start:start + 24]
        sheet = Image.new('RGB', (6 * 330, math.ceil(len(subset) / 6) * 490), '#e8e8e8')
        draw = ImageDraw.Draw(sheet)
        for index, file in enumerate(subset):
            with Image.open(file) as source:
                image = source.convert('RGB')
                image.thumbnail((310, 450))
                x, y = (index % 6) * 330, (index // 6) * 490
                draw.text((x + 10, y + 8), f'Page {start + index + 1}', fill='#111111')
                sheet.paste(image, (x + 10, y + 30))
        target = visual / f'{stem}--contact-{start // 24 + 1:02}.png'
        sheet.save(target)
        sheets.append(str(target.relative_to(base)))
    records.append({'pdf': relative, 'pages': pages, 'fullResolutionPages': selected, 'fullResolutionFiles': full, 'allPageContactSheets': sheets})
    print(json.dumps({'pdf': relative, 'pages': pages, 'selected': selected}), flush=True)
(visual / 'render-manifest.json').write_text(json.dumps({'sourceRevision': audit['sourceRevision'], 'records': records}, indent=2) + '\n')
print(json.dumps({'pdfs': len(records), 'pages': sum(record['pages'] for record in records), 'visualDirectory': str(visual)}), flush=True)
