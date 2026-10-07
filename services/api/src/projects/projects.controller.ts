import { Body, Controller, Get, Param, ParseUUIDPipe, Post, Query, Req, UseGuards } from '@nestjs/common';
import { Request } from 'express';
import { PageQueryDto } from '../common/page-query.dto';
import { SessionGuard } from '../auth/session.guard';
import { CreateProjectDto } from './dto/create-project.dto';
import { ProjectsService } from './projects.service';

@Controller('projects')
@UseGuards(SessionGuard)
export class ProjectsController {
  constructor(private readonly projects: ProjectsService) {}

  @Post()
  create(@Body() dto: CreateProjectDto, @Req() req: Request) {
    return this.projects.create(req.session.uid!, dto);
  }

  @Get()
  list(@Query() query: PageQueryDto, @Req() req: Request) {
    return this.projects.list(req.session.uid!, query);
  }

  @Get(':id')
  get(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.projects.get(req.session.uid!, id);
  }
}
